package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalith-99/echostream/internal/repository"
)

// Run against a migrated, disposable PostgreSQL database.
func TestChannelWorkspaceAccess(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	createWorkspace := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		tenant, user, err := NewSignupStore(pool).CreateTenantAndUser(ctx, "access tests",
			uuid.NewString()+"@example.test", "workspace owner", "fixture")
		if err != nil {
			t.Fatal(err)
		}
		return tenant.ID, user.ID
	}
	workspace, owner := createWorkspace()
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", workspace)
	otherWorkspace, outsider := createWorkspace()
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", otherWorkspace)
	guest, err := NewUserStore(pool).Create(ctx, workspace, uuid.NewString()+"@example.test", "guest", "fixture")
	if err != nil {
		t.Fatal(err)
	}

	directory := NewUserStore(pool)
	page, err := directory.ListByTenant(ctx, workspace, owner, 1, 0)
	if err != nil || len(page) != 1 {
		t.Fatalf("directory page: users=%v err=%v", page, err)
	}
	page, err = directory.ListByTenant(ctx, workspace, outsider, 100, 0)
	if err != nil || len(page) != 0 {
		t.Fatalf("directory viewer must belong to the workspace: users=%v err=%v", page, err)
	}

	duplicateTenant := "duplicate-" + uuid.NewString()
	if _, _, err := NewSignupStore(pool).CreateTenantAndUser(ctx, duplicateTenant, guest.Email, "duplicate", "fixture"); !errors.Is(err, repository.ErrEmailRegistered) {
		t.Fatalf("duplicate signup: %v", err)
	}
	var orphan bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM tenants WHERE name=$1)", duplicateTenant).Scan(&orphan); err != nil || orphan {
		t.Fatalf("duplicate signup must roll back tenant: orphan=%v err=%v", orphan, err)
	}

	channels, members := NewChannelStore(pool), NewMembershipStore(pool)
	private, err := channels.Create(ctx, workspace, owner, "private", true)
	if err != nil {
		t.Fatal(err)
	}
	roster, err := members.ListMembers(ctx, workspace, private.ID, owner, 10, 0)
	if err != nil || len(roster) != 1 || roster[0].UserID != owner || roster[0].Role != "admin" {
		t.Fatalf("creator should already be admin: roster=%v err=%v", roster, err)
	}
	verifyVisible := func(viewer uuid.UUID, want bool) {
		t.Helper()
		channel, err := channels.GetByID(ctx, workspace, private.ID, viewer)
		if err != nil || (channel != nil) != want {
			t.Fatalf("channel detail visibility: got=%v want=%v err=%v", channel, want, err)
		}
		list, err := channels.ListByTenant(ctx, workspace, viewer, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for _, c := range list {
			if c.ID == private.ID {
				seen = true
			}
		}
		if seen != want {
			t.Fatalf("channel list visibility: got=%v want=%v", seen, want)
		}
	}
	verifyVisible(guest.ID, false)
	if err := members.AddMember(ctx, workspace, private.ID, guest.ID, guest.ID); !errors.Is(err, repository.ErrMembershipUnavailable) {
		t.Fatalf("private channel requires invitation, got %v", err)
	}
	if err := members.AddMember(ctx, workspace, private.ID, owner, outsider); !errors.Is(err, repository.ErrMembershipUnavailable) {
		t.Fatalf("invitee must belong to workspace, got %v", err)
	}
	if err := members.AddMember(ctx, workspace, private.ID, owner, guest.ID); err != nil {
		t.Fatal(err)
	}
	if err := members.AddMember(ctx, workspace, private.ID, owner, guest.ID); err != nil {
		t.Fatalf("repeat invite: %v", err)
	}
	verifyVisible(guest.ID, true)
	// Existing installations may already contain an inconsistent membership.
	if _, err := pool.Exec(ctx, "INSERT INTO channel_members(channel_id,user_id,role) VALUES($1,$2,'member')", private.ID, outsider); err != nil {
		t.Fatal(err)
	}
	verifyVisible(outsider, false)
	allowed, err := members.IsMember(ctx, workspace, private.ID, outsider)
	if err != nil || allowed {
		t.Fatalf("membership must match both workspace identities: allowed=%v err=%v", allowed, err)
	}
	allowed, err = members.IsMember(ctx, otherWorkspace, private.ID, owner)
	if err != nil || allowed {
		t.Fatalf("membership must match signed workspace: allowed=%v err=%v", allowed, err)
	}
	roster, err = members.ListMembers(ctx, workspace, private.ID, owner, 10, 0)
	if err != nil || len(roster) != 2 {
		t.Fatalf("only workspace members should appear: roster=%v err=%v", roster, err)
	}
	if err := members.RemoveMember(ctx, private.ID, guest.ID); err != nil {
		t.Fatal(err)
	}
	verifyVisible(guest.ID, false)
	roster, err = members.ListMembers(ctx, workspace, private.ID, guest.ID, 10, 0)
	if err != nil || len(roster) != 0 {
		t.Fatalf("private roster after leaving: roster=%v err=%v", roster, err)
	}
	public, err := channels.Create(ctx, workspace, owner, "public", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := members.AddMember(ctx, workspace, public.ID, guest.ID, guest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := channels.Create(ctx, workspace, outsider, "invalid owner", false); !errors.Is(err, repository.ErrMembershipUnavailable) {
		t.Fatalf("invalid channel creator: %v", err)
	}
}
