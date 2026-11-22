-- Run only in the disposable, newly migrated database provisioned by run.sh.
INSERT INTO tenants(id,name) VALUES ('10000000-0000-0000-0000-000000000001','history bench');
INSERT INTO users(id,tenant_id,email) VALUES
 ('20000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','history@example.invalid');

INSERT INTO channels(id,tenant_id,name,is_private)
 SELECT ('30000000-0000-0000-0000-' || lpad(i::text,12,'0'))::uuid,
        '10000000-0000-0000-0000-000000000001', 'room-' || i, true
 FROM generate_series(1,16) i;
INSERT INTO channel_members(channel_id,user_id)
 SELECT id,'20000000-0000-0000-0000-000000000001' FROM channels;

-- Keep foreign-key trigger memory bounded. Interleave channel IDs to match
-- concurrent channel traffic rather than physically clustering each history.
SELECT format($insert$
 INSERT INTO messages(id,tenant_id,sender_id,channel_id,body)
 SELECT n,'10000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001',
        ('30000000-0000-0000-0000-' || lpad((1 + (n-1) %% 16)::text,12,'0'))::uuid,
        'message-' || n || repeat('x',64)
 FROM generate_series(%s,%s) n
$insert$, batch * 20000 + 1, (batch + 1) * 20000)
FROM generate_series(0,49) batch
\gexec
SELECT setval('messages_id_seq',max(id)) FROM messages;
VACUUM ANALYZE messages;
ANALYZE channels;
ANALYZE channel_members;
ANALYZE users;
SELECT version(), count(*) AS rows, pg_size_pretty(pg_total_relation_size('messages')) AS table_and_indexes FROM messages;
