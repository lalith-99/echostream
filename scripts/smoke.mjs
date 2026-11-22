// Node 22+, no npm dependencies. Run against two instances sharing one DB/Redis.
// The reader URL may be the web origin to exercise its HTTP and WebSocket proxy.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';

const [writer, reader] = process.argv.slice(2);
if (!writer || !reader || writer === reader) {
  throw new Error('Usage: node scripts/smoke.mjs <writer HTTP URL> <reader HTTP URL>');
}

async function api(base, path, status, { token, body, key } = {}) {
  const headers = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  if (key) headers['Idempotency-Key'] = key;
  if (body) headers['Content-Type'] = 'application/json';
  const response = await fetch(new URL(path, base), {
    method: body ? 'POST' : 'GET',
    headers,
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(5000),
  });
  assert.equal(response.status, status, `${path} returned HTTP ${response.status}, expected ${status}`);
  return response.json();
}

const { token } = await api(writer, '/v1/auth/signup', 201, {
  body: {
    tenant_name: 'CI',
    email: `smoke-${randomUUID()}@example.com`,
    display_name: 'CI',
    password: randomUUID(),
  },
});
assert.ok(token);
const channel = await api(writer, '/v1/channels', 201, {
  token, body: { name: 'smoke', is_private: true },
});
assert.ok(channel.id);
const path = `/v1/channels/${channel.id}/messages`;

const url = new URL('/v1/ws', reader);
url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
const ws = new WebSocket(url, ['echostream.v1', `bearer.${token}`]);
const timeout = AbortSignal.timeout(15000);

async function nextEvent(type) {
  for (;;) {
    const [frame] = await once(ws, 'message', { signal: timeout });
    const event = JSON.parse(frame.data);
    assert.notEqual(event.type, 'error', event.error);
    if (event.type === type && event.channel_id === channel.id) return event;
  }
}

try {
  await once(ws, 'open', { signal: timeout });
  assert.equal(ws.protocol, 'echostream.v1');
  const subscribed = nextEvent('subscribed');
  ws.send(JSON.stringify({ type: 'subscribe', channel_id: channel.id }));
  await subscribed;
  const delivery = nextEvent('message');
  // Keep a delayed rejection handled if the HTTP call independently fails.
  delivery.catch(() => {});
  const key = randomUUID();
  const body = { content: `across nodes ${key}` };
  const saved = await api(writer, path, 201, { token, body, key });
  assert.ok(saved.id);
  assert.equal(saved.body, body.content);
  const event = await delivery;
  assert.equal(event.message.id, saved.id);
  assert.equal(event.message.body, saved.body);
  const retry = await api(reader, path, 200, { token, body, key });
  assert.equal(retry.id, saved.id);
  await api(writer, path, 409, { token, body: { content: 'different' }, key });
  const history = await api(reader, `${path}?after=0`, 200, { token });
  assert.deepEqual(history.map((message) => message.id), [saved.id]);
  console.log('Cross-instance WebSocket delivery, retry deduplication and persisted history passed.');
} finally {
  ws.close();
}
