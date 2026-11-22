-- Existing messages retain a NULL request key. Historical messages should not
-- produce live notifications when this migration is applied.
ALTER TABLE messages ADD COLUMN request_key uuid;
CREATE UNIQUE INDEX messages_request_key_idx
  ON messages (channel_id, sender_id, request_key) WHERE request_key IS NOT NULL;

CREATE TABLE message_outbox (
  message_id bigint PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  traceparent text NOT NULL DEFAULT '',
  enqueued_at timestamptz NOT NULL DEFAULT now(),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  available_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX message_outbox_ready_idx ON message_outbox (available_at, message_id);
CREATE INDEX message_outbox_age_idx ON message_outbox (enqueued_at);
