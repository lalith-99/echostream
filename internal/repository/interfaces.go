package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/models"
)

// ChannelRepository defines the contract for channel data operations.
// All queries are scoped to a tenant for multi-tenancy isolation.
type ChannelRepository interface {
	// Create inserts the channel and its creator’s admin membership atomically.
	Create(ctx context.Context, tenantID, creatorID uuid.UUID, name string, isPrivate bool) (*models.Channel, error)

	// GetByID returns a channel visible to the viewer, or nil if it is unavailable.
	GetByID(ctx context.Context, tenantID, channelID, viewerID uuid.UUID) (*models.Channel, error)

	// ListByTenant returns public channels + private channels where userID is a member.
	ListByTenant(ctx context.Context, tenantID, userID uuid.UUID, limit, offset int) ([]models.Channel, error)
}

// MembershipRepository handles who belongs to which channel.
type MembershipRepository interface {
	// AddMember authorizes the actor to join or invite a workspace user. The new role is member.
	AddMember(ctx context.Context, tenantID, channelID, actorID, targetID uuid.UUID) error

	// RemoveMember removes a user from a channel. No-op if not a member.
	RemoveMember(ctx context.Context, channelID uuid.UUID, userID uuid.UUID) error

	// ListMembers returns members of a channel with pagination.
	ListMembers(ctx context.Context, tenantID, channelID, viewerID uuid.UUID, limit, offset int) ([]models.ChannelMember, error)

	// IsMember checks if a user belongs to a channel.
	IsMember(ctx context.Context, tenantID, channelID, userID uuid.UUID) (bool, error)
}

var (
	ErrMessageForbidden   = errors.New("channel membership required")
	ErrMessageKeyConflict = errors.New("idempotency key already used for different message content")
)

// MessagePage contains exclusive cursors. After selects ascending catch-up order.
type MessagePage struct {
	Before int64
	After  *int64
	Limit  int
}

// MessageRepository enforces tenant membership for reads and writes.
type MessageRepository interface {
	// Create writes the message and its outbox row atomically. Created is false on a matching retry.
	Create(ctx context.Context, tenantID, channelID, senderID uuid.UUID, body string, requestKey *uuid.UUID) (message *models.Message, created bool, err error)
	ListByChannel(ctx context.Context, tenantID, channelID, userID uuid.UUID, page MessagePage) ([]models.Message, error)
}

// UserRepository handles user data.
type UserRepository interface {
	// Create inserts a new user and returns it with ID and CreatedAt populated.
	Create(ctx context.Context, tenantID uuid.UUID, email, displayName, passwordHash string) (*models.User, error)

	// GetByID returns a user by their ID, scoped to the tenant.
	GetByID(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) (*models.User, error)

	// GetByEmail returns a user by email. Returns nil, nil if not found.
	GetByEmail(ctx context.Context, email string) (*models.User, error)

	// ListByTenant returns a bounded page of directory entries for a current workspace user.
	ListByTenant(ctx context.Context, tenantID, viewerID uuid.UUID, limit, offset int) ([]models.User, error)
}

// TenantRepository handles tenant (workspace) data.
type TenantRepository interface {
	Create(ctx context.Context, name string) (*models.Tenant, error)
}

// SignupRepository atomically creates a tenant + user in one operation.
// The underlying implementation uses a database transaction so that
// a failure in user creation doesn't leave orphan tenants.
type SignupRepository interface {
	CreateTenantAndUser(ctx context.Context, tenantName, email, displayName, passwordHash string) (*models.Tenant, *models.User, error)
}
