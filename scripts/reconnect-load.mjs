// Node 22+, two disposable gateways sharing PostgreSQL and Redis.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { setTimeout as sleep } from 'node:timers/promises';
import { performance } from 'node:perf_hooks';

const [a, b] = process.argv.slice(2);
for (const origin of [a,b]) assert.ok(['localhost','127.0.0.1'].includes(new URL(origin).hostname));
assert.notEqual(a,b);
const run = randomUUID();
async function request(origin, path, token, body) {
  const response = await fetch(origin + '/v1' + path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer '+token } : {}) },
    body: JSON.stringify(body), signal: AbortSignal.timeout(8000),
  });
  assert.ok(response.ok, path + ' returned ' + response.status);
  return response.status === 204 ? null : response.json();
}
async function signup(i, workspace) {
  const account = await request(a, '/auth/signup', null, {
    email: run+'-'+i+'@example.com', password: randomUUID(), display_name: 'Runner '+i, ...workspace,
  });
  return account.token;
}
const owner = await signup(0,{tenant_name:'reconnect test'});
const {id} = await request(a,'/channels',owner,{name:'test',is_private:false});
const invite = await request(a,'/workspace/invite',owner,{});
const accounts = [owner];
for (let i=1;i<5;i++) {
  const token = await signup(i,{invite_token:invite.token});
  await request(a,'/channels/'+id+'/join',token,{});
  accounts.push(token);
}
const path = '/channels/'+id+'/messages';
const started = new Map(), saved = new Map(), samples = [];
const readers = accounts.slice(2).map(token => ({token, live:new Map(), history:new Map(), duplicates:0}));
async function connect(r) {
  const ws = new WebSocket(b.replace(/^http/,'ws')+'/v1/ws',['echostream.v1','bearer.'+r.token]);
  r.ws = ws;
  const deadline = AbortSignal.timeout(8000);
  ws.addEventListener('message', e => {
    const event = JSON.parse(e.data);
    if(event.type !== 'message' || event.channel_id !== id) return;
    const msg = event.message;
    assert.ok(started.has(msg.body));
    if(r.live.has(msg.id)) { r.duplicates++; return; }
    r.live.set(msg.id,msg.body);
    samples.push(performance.now()-started.get(msg.body));
  });
  await once(ws,'open',{signal:deadline});
  ws.send(JSON.stringify({type:'subscribe',channel_id:id}));
  for (;;) {
    const [frame] = await once(ws,'message',{signal:deadline});
    const ev = JSON.parse(frame.data);
    if(ev.type === 'error') throw Error(ev.error);
    if(ev.type === 'subscribed' && ev.channel_id === id) return;
  }
}
async function close(r) {
  if(!r.ws || r.ws.readyState === WebSocket.CLOSED) return;
  const closed = once(r.ws,'close',{signal:AbortSignal.timeout(8000)});
  r.ws.close();
  await closed;
}
async function catchUp(r) {
  let cursor = r.cursor || 0;
  for (;;) {
    const page = await request(b,path+'?after='+cursor+'&limit=7',r.token);
    for(const msg of page) {
      assert.ok(msg.id > cursor);
      r.history.set(msg.id,msg.body);
      cursor = msg.id;
    }
    r.cursor = cursor;
    if(page.length < 7) return;
  }
}
try {
  await Promise.all(readers.map(connect));
  const cycling = readers[2];
  const begin = performance.now();
  let blackout = 0;
  for(let round=0;round<24;round++) {
    if(round===6) { await catchUp(cycling); await close(cycling); }
    if(round===14) {
      await connect(cycling);
      await catchUp(cycling);
      blackout = [...cycling.history.keys()].filter(key=>!cycling.live.has(key)).length;
      assert.ok(blackout>=16,'expected two offline sends in each of eight rounds');
    }
    await Promise.all([a,b].map(async (origin,index)=>{
      const content = run+'/'+round+'/'+index;
      started.set(content,performance.now());
      const row = await request(origin,path,accounts[index],{content});
      assert.equal(row.body,content);
      saved.set(row.id,content);
    }));
    await sleep(1000);
  }
  const deadline = performance.now()+8000;
  while(readers.slice(0,2).some(r=>r.live.size<48) && performance.now()<deadline) await sleep(50);
  const elapsed=performance.now()-begin;
  assert.equal(saved.size,48);
  for(const r of readers.slice(0,2)) assert.deepEqual(r.live,saved);
  const liveBeforeFinalCatchUp = readers.map(r=>r.live.size);
  for(const r of readers) {
    await catchUp(r);
    assert.deepEqual(r.history,saved);
    for(const [key,body] of r.live) assert.equal(saved.get(key),body);
  }
  samples.sort((x,y)=>x-y);
  const percentile = q=>Math.round(samples[Math.ceil(samples.length*q)-1]*10)/10;
  console.log(JSON.stringify({writers:2, readers:3, committed:48, write_window_and_drain_seconds:Math.round(elapsed/100)/10,
    live_ids_per_reader:liveBeforeFinalCatchUp, offline_ids_recovered_at_reconnect:blackout,
    duplicates_observed:readers.reduce((n,r)=>n+r.duplicates,0),missing_after_history:0,
    live_first_observation_ms:{samples:samples.length,p50:percentile(.5),p95:percentile(.95),p99:percentile(.99)}}));
} finally { await Promise.all(readers.map(close)); }
