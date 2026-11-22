DROP TABLE message_outbox;
DROP INDEX messages_request_key_idx;
ALTER TABLE messages DROP COLUMN request_key;
