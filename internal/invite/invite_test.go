package invite

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestInviteRotationCompatibility(t *testing.T) {
	dsn := os.Getenv("TEST_REDIS_URL")
	if dsn == "" {
		t.Skip("TEST_REDIS_URL is unset")
	}
	opts, err := redis.ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	workspaceID := uuid.NewString()
	svc := NewService(rdb)
	token, err := svc.Generate(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Del(ctx, tokenKey(token))
	if n, err := rdb.Exists(ctx, keyPrefix+token).Result(); err != nil || n != 0 {
		t.Fatalf("new invite should use a hashed key: %d, %v", n, err)
	}
	if ttl, err := rdb.TTL(ctx, tokenKey(token)).Result(); err != nil || ttl <= 0 || ttl > TokenTTL {
		t.Fatalf("invite TTL = %v, err=%v", ttl, err)
	}
	for i := 0; i < 2; i++ {
		id, err := svc.Resolve(ctx, token)
		if err != nil || id != workspaceID {
			t.Fatalf("resolve: %q, %v", id, err)
		}
	}
	legacy := uuid.New()
	legacyToken := base64.RawURLEncoding.EncodeToString(legacy[:])
	if err := rdb.Set(ctx, keyPrefix+legacyToken, workspaceID, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	defer rdb.Del(ctx, keyPrefix+legacyToken)
	id, err := svc.Resolve(ctx, legacyToken)
	if err != nil || id != workspaceID {
		t.Fatalf("legacy link: %q, %v", id, err)
	}
	if err := rdb.Del(ctx, keyPrefix+legacyToken).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, legacyToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing invite: %v", err)
	}
}
