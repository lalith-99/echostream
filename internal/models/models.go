package models

import (
	"time"

	"github.com/google/uuid"
)

// Tenant is the top-level isolation boundary (like a Slack workspace).
// Every user, channel, and message belongs to exactly one tenant.
// This is what makes the system "multi-tenant": company A never sees company B's data.
type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User is a person within a tenant.
//
// Why TenantID here?
//   - So every query can be scoped: "give me users WHERE tenant_id = X".
//   - Prevents cross-tenant data leaks at the query level.
//
// Why uuid.UUID and not string?
//   - Type safety. You can't accidentally pass a channel ID where a user ID
//     is expected. The field names make intent clear.
//   - In a stricter design you'd use typed IDs (type UserID uuid.UUID)
//     but that adds friction — we keep it simple for now.
//
// Why time.Time and not int64 (unix)?
//   - time.Time is what pgx naturally scans into from timestamptz.
//   - JSON marshals to RFC3339 ("2026-02-08T10:30:00Z") which frontends
//     universally understand.
type User struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// Channel is a chat room within a tenant (like #general or #incident-123).
//
// Why IsPrivate?
//   - Public channels: anyone in the tenant can discover and join.
//   - Private channels: invite-only, hidden from channel list for non-members.
//   - This one boolean drives authorization logic later.
type Channel struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	IsPrivate bool      `json:"is_private"`
	CreatedAt time.Time `json:"created_at"`
}

// ChannelMember is the join table between channels and users.
//
// Why a separate struct (not just a slice on Channel)?
//   - In Go, your struct should mirror your table. The channel_members table
//     is its own entity with its own role column.
//   - When you query "who's in this channel?", you query channel_members,
//     not channels. Separate struct = separate repository method = clean code.
//
// Role is a string, not an enum:
//   - Go doesn't have enums. You could use type Role string with constants,
//     but for now a plain string ("member", "admin") keeps it simple.
//     We validate at the handler layer, not the model layer.
type ChannelMember struct {
	ChannelID uuid.UUID `json:"channel_id"`
	UserID    uuid.UUID `json:"user_id"`
	Role      string    `json:"role"`
}

// Message is a single chat message in a channel.
//
// Why int64 for ID (not UUID)?
//   - Messages are the highest-volume table. bigserial (auto-incrementing
//     int64) is:
//     1. Smaller (8 bytes vs 16 bytes) — matters at millions of rows.
//     2. Naturally ordered — higher ID = newer message. Useful for cursors.
//     3. Index-friendly — B-tree on int64 is faster than on UUID.
//   - UUIDs are great for entities created on multiple servers
//     (users, channels). Messages always go through our API,
//     so a single sequence is fine.
//
// Why SenderID and not a nested User struct?
//   - In Go, your model matches your table. The messages table stores
//     sender_id (a foreign key), not the full user object.
//   - When the API needs to return sender info with messages, the HANDLER
//     joins the data — not the model. Models are dumb data carriers.
type Message struct {
	ID        int64     `json:"id"`
	ChannelID uuid.UUID `json:"channel_id"`
	SenderID  uuid.UUID `json:"sender_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
