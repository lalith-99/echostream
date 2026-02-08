-- Drop EchoStream initial schema (reverse order)
DROP INDEX IF EXISTS idx_messages_channel_created;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
-- Keep extension in place to avoid affecting shared DBs
-- If needed, uncomment:
-- DROP EXTENSION IF EXISTS "uuid-ossp";