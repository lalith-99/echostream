package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/models"
)

// ChannelRepository defines the contract for channel data operations.
// All queries are scoped to a tenant for multi-tenancy isolation.
type ChannelRepository interface {
	// Create inserts a new channel and returns it with ID and CreatedAt populated.
	Create(ctx context.Context, tenantID uuid.UUID, name string, isPrivate bool) (*models.Channel, error)

	// GetByID returns a single channel. Returns nil, nil if not found.
	GetByID(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID) (*models.Channel, error)

	// ListByTenant returns channels for a tenant, newest first, with pagination.
	ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]models.Channel, error)
}

// MembershipRepository handles who belongs to which channel.
type MembershipRepository interface {
	// AddMember adds a user to a channel with the given role.
	AddMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID, role string) error

	// RemoveMember removes a user from a channel. No-op if not a member.
	RemoveMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) error

	// ListMembers returns members of a channel with pagination.
	ListMembers(ctx context.Context, channelID uuid.UUID, limit, offset int) ([]models.ChannelMember, error)

	// IsMember checks if a user belongs to a channel.
	IsMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) (bool, error)
}

// MessageRepository handles chat message persistence.
type MessageRepository interface {
	// Create persists a message and returns it with ID and CreatedAt populated.
	Create(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID, senderID uuid.UUID, body string) (*models.Message, error)

	// ListByChannel returns messages in a channel, newest first, with cursor pagination.
	ListByChannel(ctx context.Context, tenantID uuid.UUID, channelID uuid.UUID, before int64, limit int) ([]models.Message, error)
}

// UserRepository handles user data.
type UserRepository interface {
	// Create inserts a new user and returns it with ID and CreatedAt populated.
	Create(ctx context.Context, tenantID uuid.UUID, email, displayName, passwordHash string) (*models.User, error)

	// GetByID returns a user by their ID, scoped to the tenant.
	GetByID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*models.User, error)

	// GetByEmail returns a user by email. Returns nil, nil if not found.
	GetByEmail(ctx context.Context, email string) (*models.User, error)
}

// TenantRepository handles tenant (workspace) data.
type TenantRepository interface {
	Create(ctx context.Context, name string) (*models.Tenant, error)
}
