package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type membershipKey struct {
	tenant, channel, user uuid.UUID
}

type membershipRequest struct {
	key   membershipKey
	reply chan membershipResult
}

type membershipResult struct {
	allowed bool
	err     error
}

// CheckSocketMembership checks membership at delivery time. Under fan-out, a socket ready
// to write can share a query with other waiting sockets. A lookup never
// admits new requests after its statement starts, and no results are cached.
func (s *MembershipStore) CheckSocketMembership(ctx context.Context, tenant, channel, user uuid.UUID) (bool, error) {
	key := membershipKey{tenant, channel, user}
	req := membershipRequest{key: key, reply: make(chan membershipResult, 1)}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case s.waiting <- req:
		return req.wait(ctx)
	case s.lookups <- struct{}{}:
	}

	// Collect only callers that are already waiting. A single reader incurs
	// no batching timer and takes the original indexed EXISTS path.
	var peers []membershipRequest
collect:
	for len(peers) < 63 {
		select {
		case peer := <-s.waiting:
			peers = append(peers, peer)
		default:
			break collect
		}
	}
	if len(peers) == 0 {
		defer func() { <-s.lookups }()
		return s.IsMember(ctx, tenant, channel, user)
	}

	// The shared lookup owns the slot until completion. Every caller can
	// return on its own deadline without aborting another socket's request.
	go s.resolveMemberships(context.WithoutCancel(ctx), append(peers, req))
	return req.wait(ctx)
}

func (r membershipRequest) wait(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-r.reply:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return result.allowed, result.err
	}
}

func (s *MembershipStore) resolveMemberships(ctx context.Context, requests []membershipRequest) {
	defer func() { <-s.lookups }()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	keys := make(map[membershipKey]bool, len(requests))
	for _, req := range requests {
		keys[req.key] = false
	}
	err := s.checkMembers(ctx, keys)
	for _, req := range requests {
		req.reply <- membershipResult{allowed: err == nil && keys[req.key], err: err}
	}
}

func (s *MembershipStore) checkMembers(ctx context.Context, keys map[membershipKey]bool) error {
	tenants := make([]uuid.UUID, 0, len(keys))
	channels := make([]uuid.UUID, 0, len(keys))
	users := make([]uuid.UUID, 0, len(keys))
	for key := range keys {
		tenants = append(tenants, key.tenant)
		channels = append(channels, key.channel)
		users = append(users, key.user)
	}
	rows, err := s.pool.Query(ctx, `SELECT k.tenant, k.channel, k.member
		FROM unnest($1::uuid[], $2::uuid[], $3::uuid[]) AS k(tenant, channel, member)
		JOIN channels c ON c.id = k.channel AND c.tenant_id = k.tenant
		JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = k.member
		JOIN users u ON u.id = cm.user_id AND u.tenant_id = c.tenant_id`, tenants, channels, users)
	if err != nil {
		return fmt.Errorf("check socket memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key membershipKey
		if err := rows.Scan(&key.tenant, &key.channel, &key.user); err != nil {
			return err
		}
		keys[key] = true
	}
	return rows.Err()
}
