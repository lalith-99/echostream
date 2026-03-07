package models

import (
	"time"

	"github.com/google/uuid"
)

// Tenant represents a workspace (top-level isolation boundary).
type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User belongs to a single tenant.
type User struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"-"` // "-" = NEVER serialize to JSON. Passwords don't leave the server.
	CreatedAt    time.Time `json:"created_at"`
}

// Channel is a chat room within a tenant.
type Channel struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	IsPrivate bool      `json:"is_private"`
	CreatedAt time.Time `json:"created_at"`
}

// ChannelMember is the join table between channels and users.
type ChannelMember struct {
	ChannelID uuid.UUID `json:"channel_id"`
	UserID    uuid.UUID `json:"user_id"`
	Role      string    `json:"role"`
}

// Message is a single chat message in a channel.
// Uses int64 ID (bigserial) instead of UUID for efficient ordering and pagination.
type Message struct {
	ID        int64     `json:"id"`
	ChannelID uuid.UUID `json:"channel_id"`
	SenderID  uuid.UUID `json:"sender_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
