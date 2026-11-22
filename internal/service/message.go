package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/lalith-99/echostream/internal/models"
	"github.com/lalith-99/echostream/internal/repository"
)

const maxMessageBody = 4000 // UTF-8 bytes

var (
	ErrInvalidBody = errors.New("message body contains a null character")
	ErrEmptyBody   = errors.New("message body is empty")
	ErrBodyTooLong = errors.New("message body exceeds maximum length")
)

type MessageService struct {
	messages repository.MessageRepository
}

func NewMessageService(messages repository.MessageRepository) *MessageService {
	return &MessageService{messages: messages}
}

// Send returns only after the message and publish intent have committed together.
func (s *MessageService) Send(ctx context.Context, tenantID, channelID, senderID uuid.UUID, body string, key *uuid.UUID) (*models.Message, bool, error) {
	if strings.TrimSpace(body) == "" {
		return nil, false, ErrEmptyBody
	}
	if len(body) > maxMessageBody {
		return nil, false, ErrBodyTooLong
	}
	if strings.IndexByte(body, 0) >= 0 {
		return nil, false, ErrInvalidBody
	}
	return s.messages.Create(ctx, tenantID, channelID, senderID, body, key)
}

func (s *MessageService) List(ctx context.Context, tenantID, channelID, userID uuid.UUID, page repository.MessagePage) ([]models.Message, error) {
	if page.Limit < 1 {
		page.Limit = 50
	}
	if page.Limit > 100 {
		page.Limit = 100
	}
	return s.messages.ListByChannel(ctx, tenantID, channelID, userID, page)
}
