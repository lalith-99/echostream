package invite

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "invite:"
	TokenTTL  = 7 * 24 * time.Hour
)

// ErrNotFound is returned when a token does not exist or has expired.
var ErrNotFound = errors.New("invite token not found or expired")

// Service manages workspace invite tokens in Redis.
// Tokens are multi-use within their TTL so a single link can onboard
// multiple team members without regenerating it each time.
type Service struct {
	rdb *goredis.Client
}

func NewService(rdb *goredis.Client) *Service {
	return &Service{rdb: rdb}
}

// Generate creates a cryptographically random token and stores it in Redis
// with tenantID as the value. Returns the token string.
func (s *Service) Generate(ctx context.Context, tenantID string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate invite token: %w", err)
	}
	token := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
	if err := s.rdb.Set(ctx, keyPrefix+token, tenantID, TokenTTL).Err(); err != nil {
		return "", fmt.Errorf("store invite token: %w", err)
	}
	return token, nil
}

// Resolve looks up a token and returns its tenant ID.
// Returns ErrNotFound if the token is missing or expired.
func (s *Service) Resolve(ctx context.Context, token string) (string, error) {
	tenantID, err := s.rdb.Get(ctx, keyPrefix+token).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("resolve invite token: %w", err)
	}
	return tenantID, nil
}
