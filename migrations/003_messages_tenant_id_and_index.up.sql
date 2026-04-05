-- Add tenant_id to messages for defense-in-depth multi-tenancy filtering.
-- Replace the wrong index (was on created_at, but queries paginate by id).

-- Step 1: Add tenant_id column (nullable initially so existing rows aren't broken)
ALTER TABLE messages ADD COLUMN tenant_id uuid REFERENCES tenants(id) ON DELETE CASCADE;

-- Step 2: Backfill tenant_id from the channel's tenant_id
UPDATE messages
SET tenant_id = channels.tenant_id
FROM channels
WHERE messages.channel_id = channels.id;

-- Step 3: Make it NOT NULL now that all rows are populated
ALTER TABLE messages ALTER COLUMN tenant_id SET NOT NULL;

-- Step 4: Drop the wrong index and create the correct one
DROP INDEX IF EXISTS idx_messages_channel_created;
CREATE INDEX IF NOT EXISTS idx_messages_channel_id
  ON messages (channel_id, id DESC);
