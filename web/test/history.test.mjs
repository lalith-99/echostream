import assert from 'node:assert/strict';
import test from 'node:test';
import { LIVE_WINDOW, mergeLiveMessages, mergeMessages, PAGE_SIZE, reconcileHistory } from '../src/lib/history.ts';

const row = (id) => ({ id, body: String(id), channel_id: 'general', sender_id: 'a', created_at: '2026-01-01T00:00:00Z' });
const ids = (rows) => rows.map((r) => r.id);

test('HTTP acknowledgment, redelivered live frames, and overlapping older pages share one ordered timeline', () => {
  const initial = [row(12), row(15)];
  const merged = mergeMessages(initial, [row(20), row(12), row(8), row(15), row(20), row(13)]);
  assert.deepEqual(ids(merged), [8, 12, 13, 15, 20]);
  assert.deepEqual(ids(initial), [12, 15]);
});

test('a live frame ahead of a delayed initial history read does not move its durable cursor', async () => {
  let timeline = [row(120)];
  let watermark = null;
  const apply = (page, cursor) => {
    timeline = mergeMessages(timeline, page);
    watermark = cursor;
  };
  const signal = new AbortController().signal;
  await reconcileHistory(watermark, async (cursor) => {
    assert.deepEqual(cursor, {});
    return [row(104), row(100)];
  }, apply, signal);
  assert.equal(watermark, 104);
  await reconcileHistory(watermark, async (cursor) => {
    assert.deepEqual(cursor, { after: 104 });
    return [row(105), row(111), row(120)];
  }, apply, signal);
  assert.equal(watermark, 120);
  assert.deepEqual(ids(timeline), [100, 104, 105, 111, 120]);
});

test('reconnect recovers multiple pages, resumes after partial failure, and permits nonconsecutive IDs', async () => {
  const page = Array.from({ length: PAGE_SIZE }, (_, i) => row(1010 + i * 2));
  let watermark = 1000;
  const timeline = [];
  const apply = (rows, cursor) => { timeline.push(...rows); watermark = cursor; };
  const signal = new AbortController().signal;
  await assert.rejects(() => reconcileHistory(watermark, async ({ after }) => {
    if (after === 1000) return page;
    throw new Error('temporary failure');
  }, apply, signal), /temporary failure/);
  assert.equal(watermark, page.at(-1).id);
  await reconcileHistory(watermark, async ({ after }) => {
    assert.equal(after, page.at(-1).id);
    return [row(after + 20)];
  }, apply, signal);
  assert.equal(timeline.length, PAGE_SIZE + 1);
  assert.equal(watermark, page.at(-1).id + 20);
});

test('an abandoned channel discards delayed reads; an empty baseline starts at zero', async () => {
  const ctrl = new AbortController();
  await reconcileHistory(null, async () => { ctrl.abort(); return [row(1)]; },
    () => assert.fail('channel already closed'), ctrl.signal);
  await reconcileHistory(null, async () => [], (rows, cursor, initial) => {
    assert.deepEqual(rows, []);
    assert.equal(cursor, 0);
    assert.equal(initial, true);
  }, new AbortController().signal);
});


test('a long-lived channel bounds its live window and ignores late replays outside it', () => {
  const stream = Array.from({ length: LIVE_WINDOW * 3 }, (_, i) => row(i + 1));
  const live = mergeLiveMessages([], stream);
  assert.equal(live.length, LIVE_WINDOW);
  assert.equal(live[0].id, LIVE_WINDOW * 2 + 1);
  assert.deepEqual(ids(mergeLiveMessages(live, [row(2), row(3)])), ids(live));
});
