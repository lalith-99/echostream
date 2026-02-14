package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/models"
)

// Why context.Context as the first parameter on every method?
//
//   - It's idiomatic Go for anything that does I/O (DB, Redis, HTTP).
//   - It carries deadlines: if the HTTP request is cancelled (client
//     disconnected), the DB query gets cancelled too. No wasted work.
//   - It carries tracing spans: OpenTelemetry propagates trace IDs through
//     context, so you get end-to-end traces for free.
//   - Rule of thumb in Go: if a function touches the network, it takes ctx.

// Why tenantID appears in almost every method signature?
//
//   - Multi-tenancy safety. Every query MUST be scoped to a tenant.
//   - Even if someone guesses a channel UUID, they can't access it unless
//     their tenantID matches. This is defense-in-depth at the data layer.
//   - The handler extracts tenantID from the JWT and passes it down.
//     The repository never trusts the caller — it always filters by tenant.

// ChannelRepository defines the contract for channel data operations.
type ChannelRepository interface {
	// Create inserts a new channel and returns it with ID and CreatedAt populated.
	Create(ctx context.Context, tenantID uuid.UUID, name string, isPrivate bool) (*models.Channel, error)

	// GetByID returns a single channel. Returns nil, nil if not found.
	GetByID(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID) (*models.Channel, error)

	// ListByTenant returns all channels the tenant has, newest first.
	// Returns empty slice (not nil) so JSON serializes to [] not null.
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]models.Channel, error)
}

// MembershipRepository handles who belongs to which channel.
type MembershipRepository interface {
	// AddMember adds a user to a channel with the given role.
	AddMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID, role string) error

	// RemoveMember removes a user from a channel. No-op if not a member.
	RemoveMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) error

	// ListMembers returns all members of a channel.
	ListMembers(ctx context.Context, channelID uuid.UUID) ([]models.ChannelMember, error)

	// IsMember checks if a user belongs to a channel. Hot-path check —
	// called before every message send and WS subscribe.
	IsMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) (bool, error)
}

// MessageRepository handles chat message persistence.
type MessageRepository interface {
	// Create persists a message and returns it with ID and CreatedAt populated.
	Create(ctx context.Context, channelID uuid.UUID, senderID uuid.UUID, body string) (*models.Message, error)

	// ListByChannel returns messages in a channel, newest first.
	// Uses cursor-based pagination: before=0 means "from the top" (latest).
	ListByChannel(ctx context.Context, channelID uuid.UUID, before int64, limit int) ([]models.Message, error)
}

// UserRepository handles user data.
type UserRepository interface {
	// GetByID returns a user by their ID, scoped to the tenant.
	GetByID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*models.User, error)
}
