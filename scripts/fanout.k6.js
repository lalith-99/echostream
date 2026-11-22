// k6 v2.4.0. One coordinator counts each (reader, durable message ID) pair.
import http from 'k6/http';
import { sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { WebSocket } from 'k6/websockets';

const writer = __ENV.WRITER || 'http://127.0.0.1:8081';
const reader = __ENV.READER || 'http://127.0.0.1:8082';
const readers = Number(__ENV.READERS || 20);
const messages = Number(__ENV.MESSAGES || 10);
const expected = readers * messages;
const intervalMs = 100;
const loopback = /^http:\/\/(127\.0\.0\.1|localhost):\d+$/;
if (!loopback.test(writer) || !loopback.test(reader) || writer === reader ||
    !Number.isInteger(readers) || readers < 2 || readers > 30 ||
    !Number.isInteger(messages) || messages < 1 || messages > 30) {
  throw new Error('Use two distinct loopback HTTP addresses, 2–30 readers and 1–30 messages.');
}

const connected = new Counter('subscribed_readers');
const accepted = new Counter('accepted_writes');
const deliveries = new Counter('unique_deliveries');
const duplicates = new Counter('duplicate_deliveries');
const invalid = new Counter('invalid_results');
const latency = new Trend('post_to_websocket_ms', true);

export const options = {
  vus: 1, iterations: 1, setupTimeout: '4m',
  summaryTrendStats: ['min', 'med', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    subscribed_readers: [`count==${readers}`],
    accepted_writes: [`count==${messages}`],
    unique_deliveries: [`count==${expected}`],
    invalid_results: ['count==0'],
  },
};

function params(token) {
  return { headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) } };
}

function provision(path, body, token, status = 201) {
  const url = writer + path;
  let res = http.post(url, JSON.stringify(body), params(token));
  if (res.status === 429) {
    sleep(Math.min(61, Number(res.headers['Retry-After']) || 60));
    res = http.post(url, JSON.stringify(body), params(token));
  }
  if (res.status !== status) throw new Error(`Setup ${path} returned HTTP ${res.status}`);
  return res.body ? res.json() : null;
}

export function setup() {
  const run = crypto.randomUUID();
  const signup = (i, workspace) => provision('/v1/auth/signup', {
    ...workspace, email: `${run}-${i}@example.com`, display_name: `Reader ${i}`, password: crypto.randomUUID(),
  }).token;
  const owner = signup(0, { tenant_name: 'k6 fanout' });
  const channel = provision('/v1/channels', { name: 'fanout', is_private: false }, owner).id;
  const invite = provision('/v1/workspace/invite', {}, owner).token;
  const tokens = [owner];
  for (let i = 1; i < readers; i++) {
    const token = signup(i, { invite_token: invite });
    provision(`/v1/channels/${channel}/join`, {}, token, 204);
    tokens.push(token);
  }
  return { tokens, channel, run };
}

export default function ({ tokens, channel, run }) {
  const started = new Map();
  const posted = new Map();
  const observed = new Map();
  const seen = new Set();
  let subscribed = 0;
  let attempts = 0;
  let responses = 0;
  let completed = false;
  let interval;
  const sockets = [];
  // Emit zero samples so thresholds and the JSON summary also cover failures.
  for (const counter of [connected, accepted, deliveries, duplicates, invalid]) counter.add(0);

  function finish() {
    if (completed) return;
    completed = true;
    clearTimeout(deadline);
    if (interval) clearInterval(interval);
    for (const socket of sockets) {
      if (socket.readyState === socket.OPEN) socket.close();
    }
  }

  function maybeDone() {
    if (seen.size === expected && responses === messages) finish();
  }

  function send() {
    const body = `${run}/${++attempts}`;
    started.set(body, Date.now());
    http.asyncRequest('POST', `${writer}/v1/channels/${channel}/messages`,
      JSON.stringify({ content: body }), params(tokens[0])).then((res) => {
      responses++;
      if (res.status === 201) {
        const id = res.json('id');
        posted.set(body, id);
        accepted.add(1);
        if (!id || (observed.has(body) && observed.get(body) !== id)) invalid.add(1);
      } else {
        invalid.add(1);
      }
      maybeDone();
    }).catch(() => { responses++; invalid.add(1); });
    if (attempts === messages) clearInterval(interval);
  }

  const deadline = setTimeout(finish, 20000);
  for (const [i, token] of tokens.entries()) {
    const socket = new WebSocket(reader.replace('http:', 'ws:') + '/v1/ws', [], params(token));
    sockets.push(socket);
    let ready = false;
    socket.onopen = () => {
      if (completed) { socket.close(); return; }
      socket.send(JSON.stringify({ type: 'subscribe', channel_id: channel }));
    };
    socket.onerror = () => invalid.add(1);
    socket.onmessage = ({ data }) => {
      const event = JSON.parse(data);
      if (event.type === 'error') { invalid.add(1); return; }
      if (event.channel_id !== channel) return;
      if (event.type === 'subscribed' && !ready) {
        ready = true;
        connected.add(1);
        if (++subscribed === readers) interval = setInterval(send, intervalMs);
      }
      if (event.type !== 'message') return;
      const { body, id } = event.message;
      if (!started.has(body) || !id ||
          (posted.has(body) && posted.get(body) !== id) ||
          (observed.has(body) && observed.get(body) !== id)) {
        invalid.add(1);
        return;
      }
      observed.set(body, id);
      const pair = `${i}/${id}`;
      if (seen.has(pair)) { duplicates.add(1); return; }
      seen.add(pair);
      deliveries.add(1);
      latency.add(Date.now() - started.get(body));
      maybeDone();
    };
  }
}

export function handleSummary(data) {
  const count = (name) => data.metrics[name]?.values.count || 0;
  const result = {
    readers, messages, issue_interval_ms: intervalMs, expected_deliveries: expected,
    subscribed_readers: count('subscribed_readers'), accepted_writes: count('accepted_writes'),
    unique_deliveries: count('unique_deliveries'), missing_deliveries: expected - count('unique_deliveries'),
    duplicate_deliveries_observed: count('duplicate_deliveries'), invalid_results: count('invalid_results'),
    post_to_websocket_ms: data.metrics.post_to_websocket_ms?.values || null,
  };
  const json = JSON.stringify(result, null, 2) + '\n';
  return { stdout: json, ...(__ENV.RESULT ? { [__ENV.RESULT]: json } : {}) };
}
