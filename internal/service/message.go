package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/repository"
	"github.com/lalith-99/echostream/internal/websocket"
	"go.uber.org/zap"
)

const (
	maxMessageBody      = 4000 // bytes
	defaultMessageLimit = 50
	maxMessageLimit     = 100
	messageEventType    = "message"
	channelTopicPrefix  = "ch:"
)

// Sentinel errors the handler can check with errors.Is().
var (
	ErrNotMember   = errors.New("sender is not a member of this channel")
	ErrEmptyBody   = errors.New("message body is empty")
	ErrBodyTooLong = errors.New("message body exceeds maximum length")
)

// EventPublisher pushes events to a pub/sub system (e.g., Redis).
type EventPublisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

// MessageService owns the business rules for sending and listing messages.
// It sits between the HTTP handler and the repositories.
type MessageService struct {
	messages   repository.MessageRepository
	membership repository.MembershipRepository
	publisher  EventPublisher
	logger     *zap.Logger
}

// NewMessageService builds a MessageService.
func NewMessageService(
	messages repository.MessageRepository,
	membership repository.MembershipRepository,
	publisher EventPublisher,
	logger *zap.Logger,
) *MessageService {
	return &MessageService{
		messages:   messages,
		membership: membership,
		publisher:  publisher,
		logger:     logger,
	}
}

// Send validates, persists, and publishes a message.
//
// Business rules enforced here:
//  1. Body must not be empty
//  2. Body must not exceed maxMessageBody bytes
//  3. Sender must be a member of the channel
//
// If persist succeeds but publish fails, we log and move on.
// The message is already saved — real-time delivery is best-effort.
func (s *MessageService) Send(ctx context.Context, tenantID, channelID, senderID uuid.UUID, body string) (*models.Message, error) {
	// Rule 1: no empty messages
	if body == "" {
		return nil, ErrEmptyBody
	}
	// Rule 2: cap message size
	if len(body) > maxMessageBody {
		return nil, ErrBodyTooLong
	}

	// Rule 3: only channel members can post
	ok, err := s.membership.IsMember(ctx, channelID, senderID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMember
	}

	// Persist to Postgres
	msg, err := s.messages.Create(ctx, tenantID, channelID, senderID, body)
	if err != nil {
		return nil, err
	}

	// Fan out via Redis for real-time WebSocket delivery
	if s.publisher != nil {
		event := websocket.OutboundEvent{
			Type:      messageEventType,
			ChannelID: channelID.String(),
			Message:   msg,
		}
		data, _ := json.Marshal(event)
		if err := s.publisher.Publish(ctx, channelTopicPrefix+channelID.String(), data); err != nil {
			s.logger.Error("publish failed (message is saved, delivery is best-effort)",
				zap.Error(err),
				zap.String("channel_id", channelID.String()),
			)
		}
	}

	return msg, nil
}

// List returns messages with cursor pagination. Applies default/max limits.
func (s *MessageService) List(ctx context.Context, tenantID, channelID uuid.UUID, before int64, limit int) ([]models.Message, error) {
	if limit < 1 {
		limit = defaultMessageLimit
	}
	if limit > maxMessageLimit {
		limit = maxMessageLimit
	}
	return s.messages.ListByChannel(ctx, tenantID, channelID, before, limit)
}
