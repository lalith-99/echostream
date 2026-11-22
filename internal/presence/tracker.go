package presence

import (
	"context"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	keyTTL          = 90 * time.Second
	refreshInterval = 30 * time.Second
	storeTimeout    = 2 * time.Second
)

type Status string

const (
	Online  Status = "online"
	Offline Status = "offline"
)

// Each socket owns a separate expiring lease. Redis supplies the clock for
// both reads and writes so processes do not need synchronized clocks.
var renewLease = goredis.NewScript(`
local now = tonumber(redis.call('TIME')[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
`)

var readLeases = goredis.NewScript(`
local now = tonumber(redis.call('TIME')[1])
local result = {}
for i, key in ipairs(KEYS) do
    result[i] = redis.call('ZCOUNT', key, '(' .. now, '+inf')
end
return result
`)

type Tracker struct {
	rdb    *goredis.Client
	logger *zap.Logger
}

func NewTracker(rdb *goredis.Client, logger *zap.Logger) *Tracker {
	return &Tracker{rdb: rdb, logger: logger}
}

func presenceKey(userID uuid.UUID) string {
	return "presence:leases:" + userID.String()
}

func (t *Tracker) renew(ctx context.Context, userID, leaseID uuid.UUID) error {
	return renewLease.Run(ctx, t.rdb, []string{presenceKey(userID)}, leaseID.String(), int(keyTTL/time.Second)).Err()
}

// Track refreshes only this socket's lease. A normal close removes it
// immediately; after a crash the score and key expiry bound stale presence.
func (t *Tracker) Track(ctx context.Context, userID uuid.UUID) {
	leaseID := uuid.New()
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		writeCtx, cancel := context.WithTimeout(ctx, storeTimeout)
		err := t.renew(writeCtx, userID, leaseID)
		cancel()
		if err != nil && ctx.Err() == nil {
			t.logger.Warn("presence lease refresh failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}

	cleanup, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if err := t.rdb.ZRem(cleanup, presenceKey(userID), leaseID.String()).Err(); err != nil {
		t.logger.Warn("presence lease cleanup failed", zap.Error(err))
	}
}

func (t *Tracker) BulkStatus(ctx context.Context, userIDs []uuid.UUID) map[uuid.UUID]Status {
	result := make(map[uuid.UUID]Status, len(userIDs))
	if len(userIDs) == 0 {
		return result
	}
	keys := make([]string, len(userIDs))
	for i, id := range userIDs {
		keys[i] = presenceKey(id)
	}
	counts, err := readLeases.Run(ctx, t.rdb, keys).Int64Slice()
	if err != nil {
		t.logger.Warn("presence lookup failed", zap.Error(err))
		return nil
	}
	for i, id := range userIDs {
		result[id] = Offline
		if counts[i] > 0 {
			result[id] = Online
		}
	}
	return result
}
