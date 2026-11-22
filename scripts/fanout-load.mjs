// Node 22+. Both API origins must belong to the same disposable Compose stack.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { availableParallelism, cpus, totalmem } from 'node:os';
import { setTimeout as sleep } from 'node:timers/promises';

const origins = process.argv.slice(2);
assert.equal(origins.length, 2, 'pass two loopback API origins');
for (const origin of origins) {
  const url = new URL(origin);
  assert.ok(url.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(url.hostname));
}
assert.notEqual(origins[0], origins[1]);

const writers = 16, connections = 256, rounds = 42, trials = 3, intervalMs = 1500;
const source = randomUUID(), tokens = [], sockets = [], records = new Map();
let shutdown = false, disconnects = 0, socketErrors = 0, malformed = 0;
let channel;
async function request(origin, path, token, body) {
  const response = await fetch(origin + '/v1' + path, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...(token && {authorization: 'Bearer ' + token}) },
    body: JSON.stringify(body), signal: AbortSignal.timeout(15000)
  });
  if (!response.ok) throw new Error('HTTP ' + response.status);
  return response.status === 204 ? null : response.json();
}

async function setup() {
  const signup = (i, args) => request(origins[0], '/auth/signup', null, {
    email: source+'-'+i+'@example.com', password: randomUUID(), display_name: 'Load '+i, ...args
  });
  tokens.push((await signup(0, {tenant_name: 'fanout benchmark'})).token);
  channel = (await request(origins[0], '/channels', tokens[0], {name: 'load', is_private: false})).id;
  const invite = await request(origins[0], '/workspace/invite', tokens[0], {});
  for (let i = 1; i < writers; i++) {
    const {token} = await signup(i, {invite_token: invite.token});
    await request(origins[0], '/channels/'+channel+'/join', token, {});
    tokens.push(token);
  }
}

function subscribe(index) {
  // Eight sockets per account per gateway. Half of each writer's readers use the other gateway.
  const origin = origins[Math.floor(index / writers) % 2];
  const socket = new WebSocket(origin.replace('http:', 'ws:')+'/v1/ws',
    ['echostream.v1', 'bearer.'+tokens[index % writers]]);
  sockets.push(socket);
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error('subscription timed out')), 15000);
    socket.onopen = () => socket.send(JSON.stringify({type:'subscribe', channel_id:channel}));
    socket.onclose = () => {
      clearTimeout(timeout);
      if (!shutdown) { disconnects++; reject(new Error('socket closed')); }
    };
    socket.onerror = () => { socketErrors++; reject(new Error('socket error')); };
    socket.onmessage = ({data}) => {
      const event = JSON.parse(data);
      if (event.type === 'error') { socketErrors++; reject(new Error('server error frame')); return; }
      if (event.channel_id !== channel) return;
      if (event.type === 'subscribed') { clearTimeout(timeout); resolve(); return; }
      if (event.type !== 'message') return;
      const item = records.get(event.message.body);
      if (!item || !Number.isSafeInteger(event.message.id)) { malformed++; return; }
      if (item.ids[index]) { item.duplicates++; return; }
      item.ids[index] = event.message.id;
      item.latencies.push(performance.now() - item.start);
    };
  });
}

async function post(t, round, writer) {
  const body = (source+'/'+t+'/'+round+'/'+writer+'/').padEnd(512, 'x');
  const item = {trial:t, start:performance.now(), ids:new Float64Array(connections),
    latencies:[], duplicates:0, id:null, error:null};
  records.set(body, item);
  try {
    const row = await request(origins[writer % 2], '/channels/'+channel+'/messages', tokens[writer], {content:body});
    assert.ok(Number.isSafeInteger(row.id) && row.id > 0 && row.body === body);
    item.id = row.id;
    item.ackMs = performance.now() - item.start;
  } catch (error) {
    item.error = error.message;
  }
}

function percentiles(values) {
  values.sort((a,b) => a-b);
  const quantile = p => values.length ? +values[Math.ceil(p*values.length)-1].toFixed(2) : null;
  return {samples:values.length, p50:quantile(.50), p95:quantile(.95), p99:quantile(.99), max:quantile(1)};
}

async function drive(t) {
  const begin = performance.now(), writes = [], lag = [];
  for (let round = 0; round < rounds; round++) {
    const scheduled = begin + round*intervalMs;
    await sleep(Math.max(0, scheduled - performance.now()));
    lag.push(Math.max(0, performance.now() - scheduled));
    for (let writer = 0; writer < writers; writer++) writes.push(post(t, round, writer));
  }
  await Promise.all(writes);
  const items = [...records.values()].filter(item => item.trial === t && item.id);
  const deadline = performance.now()+10000;
  while (items.some(item => item.latencies.length < connections) && performance.now() < deadline) {
    await sleep(10);
  }
  return {elapsed_ms:+(performance.now()-begin).toFixed(1), scheduler_lag_ms:percentiles(lag)};
}

function summarize(t, timing) {
  const items = [...records.values()].filter(item => item.trial === t);
  const accepted = items.filter(item => item.id), errors = {};
  let missing = 0, mismatches = 0;
  for (const item of items) {
    if (item.error) errors[item.error] = (errors[item.error] || 0)+1;
    for (const observed of item.ids) {
      if (item.id && !observed) missing++;
      if (observed && observed !== item.id) mismatches++;
    }
  }
  const expected = accepted.length*connections;
  const delivered = expected-missing-mismatches;
  return {trial:t+1, ...timing, attempted:items.length, acknowledged:accepted.length,
    unique_ack_ids:new Set(accepted.map(item => item.id)).size, verified_pairs:delivered, missing, mismatches,
    duplicates:items.reduce((sum,item) => sum+item.duplicates,0), http_errors:errors,
    verified_pairs_per_second:+(delivered*1000/timing.elapsed_ms).toFixed(1),
    http_ack_ms:percentiles(accepted.map(item=>item.ackMs)),
    live_observation_ms:percentiles(accepted.flatMap(item=>item.latencies))};
}

function cgroup(name) {
  try { return readFileSync('/sys/fs/cgroup/'+name,'utf8').trim(); } catch { return null; }
}
try {
  await setup();
  // Ramp subscription setup to avoid mixing join-driven presence traffic into the trial.
  for (let i = 0; i < connections; i += 4) {
    await Promise.all(Array.from({length:4}, (_,k) => subscribe(i+k)));
    await sleep(150);
  }
  await sleep(1500);
  const timings = [];
  for (let t = 0; t < trials; t++) timings.push(await drive(t));
  // Include duplicate frames arriving after the last expected ID.
  await sleep(2000);
  const result = {commit:process.env.BENCH_COMMIT || null, run:process.env.GITHUB_RUN_ID || null,
    node:process.version, platform:process.platform, arch:process.arch, cpu:cpus()[0]?.model,
    cpus_available:availableParallelism(), total_memory_bytes:totalmem(),
    cpu_quota:cgroup('cpu.max'), memory_limit_bytes:cgroup('memory.max'),
    gateways:2, accounts:writers, sockets:connections, payload_bytes:512, rounds, interval_ms:intervalMs,
    disconnects, socket_errors:socketErrors, unexpected_frames:malformed, trials:timings.map((timing,t)=>summarize(t,timing))};
  const json = JSON.stringify(result,null,2);
  console.log(json);
  if (process.env.RESULT) writeFileSync(process.env.RESULT,json+'\n');
  assert.equal(disconnects+socketErrors+malformed,0);
  for (const run of result.trials) {
    assert.equal(run.unique_ack_ids,rounds*writers);
    assert.equal(run.verified_pairs,rounds*writers*connections);
    assert.equal(run.missing+run.mismatches+run.duplicates,0);
  }
} finally {
  shutdown = true;
  for (const socket of sockets) socket.close();
}
