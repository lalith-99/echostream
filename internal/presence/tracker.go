package presence

import (
	"context"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	presenceKeyPrefix = "presence:"

	// How long a presence key lives before it's considered stale.
	// Must be longer than the refresh interval.
	keyTTL = 90 * time.Second

	// How often we refresh the TTL while the user is connected.
	refreshInterval = 30 * time.Second
)

// Status represents a user's online state.
type Status string

const (
	statusOnline  = "online"
	statusOffline = "offline"
)

const (
	Online  Status = statusOnline
	Offline Status = statusOffline
)

// Tracker manages user presence state in Redis.
//
// Each online user has a Redis key: "presence:<userID>" = "online"
// with a TTL. If the server crashes without cleanup, the key
// expires naturally — no stale "ghost online" users.
type Tracker struct {
	rdb    *goredis.Client
	logger *zap.Logger
}

// NewTracker creates a presence tracker backed by Redis.
func NewTracker(rdb *goredis.Client, logger *zap.Logger) *Tracker {
	return &Tracker{rdb: rdb, logger: logger}
}

func presenceKey(userID uuid.UUID) string {
	return presenceKeyPrefix + userID.String()
}

// SetOnline marks a user as online with a TTL.
// Call this when a WebSocket client registers.
func (t *Tracker) SetOnline(ctx context.Context, userID uuid.UUID) {
	key := presenceKey(userID)
	if err := t.rdb.Set(ctx, key, string(Online), keyTTL).Err(); err != nil {
		t.logger.Error("presence set failed", zap.Error(err))
	}
}

// SetOffline removes the user's presence key.
// Call this when a WebSocket client disconnects.
func (t *Tracker) SetOffline(ctx context.Context, userID uuid.UUID) {
	key := presenceKey(userID)
	if err := t.rdb.Del(ctx, key).Err(); err != nil {
		t.logger.Error("presence delete failed", zap.Error(err))
	}
}

// IsOnline checks if a single user is currently online.
func (t *Tracker) IsOnline(ctx context.Context, userID uuid.UUID) bool {
	val, err := t.rdb.Get(ctx, presenceKey(userID)).Result()
	if err != nil {
		return false
	}
	return val == string(Online)
}

// BulkStatus returns the presence status for a list of user IDs.
// Uses Redis MGET for efficiency — one round trip for N users.
func (t *Tracker) BulkStatus(ctx context.Context, userIDs []uuid.UUID) map[uuid.UUID]Status {
	if len(userIDs) == 0 {
		return nil
	}

	keys := make([]string, len(userIDs))
	for i, id := range userIDs {
		keys[i] = presenceKey(id)
	}

	// MGET returns values in the same order as keys.
	// Missing keys come back as nil (redis.Nil).
	vals, err := t.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		t.logger.Error("presence bulk get failed", zap.Error(err))
		return nil
	}

	result := make(map[uuid.UUID]Status, len(userIDs))
	for i, id := range userIDs {
		if vals[i] != nil {
			result[id] = Online
		} else {
			result[id] = Offline
		}
	}
	return result
}

// KeepAlive refreshes the TTL for a connected user in a loop.
// Run this as a goroutine per connected client. It stops when ctx is cancelled
// (i.e., when the client disconnects and we cancel their context).
func (t *Tracker) KeepAlive(ctx context.Context, userID uuid.UUID) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			key := presenceKey(userID)
			if err := t.rdb.Expire(ctx, key, keyTTL).Err(); err != nil {
				t.logger.Debug("presence refresh failed", zap.Error(err))
			}
		}
	}
}
