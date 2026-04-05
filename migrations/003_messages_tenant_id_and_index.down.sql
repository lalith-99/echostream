-- Revert: remove tenant_id from messages, restore original index.

DROP INDEX IF EXISTS idx_messages_channel_id;

ALTER TABLE messages DROP COLUMN IF EXISTS tenant_id;

CREATE INDEX IF NOT EXISTS idx_messages_channel_created
  ON messages (channel_id, created_at DESC);
