// Run only against a disposable local Compose stack. This test briefly stops
// its Redis service and leaves the database and both API processes running.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { setTimeout as sleep } from 'node:timers/promises';

const [writer, reader] = process.argv.slice(2);
if (!writer || !reader || writer === reader || [reader, writer].some(base =>
  !['127.0.0.1', 'localhost'].includes(new URL(base).hostname))) {
  throw new Error('Usage: node scripts/recovery.mjs <local writer URL> <local reader URL>');
}

function compose(...args) {
  return execFileSync('docker', ['compose', ...args], { encoding: 'utf8', timeout: 30000 });
}

function query(sql) {
  return compose('exec', '-T', 'postgres', 'sh', '-c',
    'exec psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "$1"', 'sh', sql).trim();
}

async function eventually(condition, description) {
  const end = Date.now() + 30000;
  while (Date.now() < end) {
    if (await condition()) return;
    await sleep(150);
  }
  throw new Error('Timed out waiting for ' + description);
}

async function api(base, path, status, { token, body, key } = {}) {
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers.Authorization = 'Bearer ' + token;
  if (key) headers['Idempotency-Key'] = key;
  const response = await fetch(new URL(path, base), {
    method: body ? 'POST' : 'GET', headers, body: body && JSON.stringify(body),
    signal: AbortSignal.timeout(10000),
  });
  assert.equal(response.status, status, path);
  return response.json();
}

async function subscribe(token, id) {
  const url = new URL('/v1/ws', reader);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  const socket = new WebSocket(url, ['echostream.v1', 'bearer.' + token]);
  const events = [];
  socket.addEventListener('message', frame => events.push(JSON.parse(frame.data)));
  try {
    await once(socket, 'open', { signal: AbortSignal.timeout(10000) });
    socket.send(JSON.stringify({ type: 'subscribe', channel_id: id }));
    await eventually(() => events.some(e => e.type === 'subscribed' && e.channel_id === id), 'channel subscription');
    return { socket, events };
  } catch (err) {
    socket.close();
    throw err;
  }
}

const { token } = await api(writer, '/v1/auth/signup', 201, { body: {
  tenant_name: 'recovery', display_name: 'test', email: randomUUID() + '@example.com', password: randomUUID(),
} });
const channel = await api(writer, '/v1/channels', 201, { token, body: { name: 'recovery', is_private: true } });
const path = '/v1/channels/' + channel.id + '/messages';
const post = (node, content, key = randomUUID(), status = 201) =>
  api(node, path, status, { token, body: { content }, key });
const history = after => api(reader, path + '?after=' + after, 200, { token });

const { socket, events } = await subscribe(token, channel.id);
try {
  const baseline = await post(writer, 'before interruption');
  await eventually(() => events.some(e => e.message?.id === baseline.id), 'initial cross-instance delivery');

  let stored;
  try {
    compose('stop', 'redis');
    await api(writer, '/v1/ready', 503);
    const key = randomUUID();
    stored = await post(writer, 'during interruption', key);
    assert.ok(Number.isSafeInteger(stored.id));
    const replay = await post(reader, 'during interruption', key, 200);
    assert.equal(replay.id, stored.id);
    assert.deepEqual((await history(baseline.id)).map(m => m.id), [stored.id]);
    await eventually(() => Number(query('SELECT attempts FROM message_outbox WHERE message_id=' + stored.id)) > 0, 'failed relay attempt');
  } finally {
    compose('start', 'redis');
  }

  await eventually(async () => (await Promise.all([writer, reader].map(base =>
    fetch(new URL('/v1/ready', base), { signal: AbortSignal.timeout(5000) })))).every(r => r.ok), 'both gateways to become ready');
  await eventually(() => events.some(e => e.type === 'resync' && e.channel_id === channel.id), 'resync on the original socket');
  await eventually(() => query('SELECT count(*) FROM message_outbox WHERE message_id=' + stored.id) === '0', 'outbox drain');
  assert.deepEqual((await history(baseline.id)).map(m => m.id), [stored.id]);

  const closed = once(socket, 'close', { signal: AbortSignal.timeout(5000) });
  socket.close();
  await closed;
  const first = await post(reader, 'offline one');
  const second = await post(writer, 'offline two');
  const reconnected = await subscribe(token, channel.id);
  try {
    assert.deepEqual((await history(stored.id)).map(m => m.id), [first.id, second.id]);
  } finally {
    reconnected.socket.close();
  }
  console.log('Redis restart, subscription resync, durable retry and ordered cross-instance gap recovery passed.');
} finally {
  socket.close();
}
