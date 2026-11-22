import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import test from 'node:test';
import { chromium } from 'playwright-core';

const origin = new URL(process.env.ECHO_BROWSER_URL ?? 'http://localhost:5173').origin;

async function signup(page, displayName, email, workspace) {
  if (workspace) await page.getByLabel('Workspace name').fill(workspace);
  await page.getByLabel('Display name').fill(displayName);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('test-' + randomUUID());
  await page.getByRole('button', { name: workspace ? 'Create workspace' : 'Join workspace' }).click();
  await page.getByRole('button', { name: 'Invite to workspace' }).waitFor();
}

function observeFrames(page) {
  const frames = [];
  page.on('websocket', (socket) => {
    socket.on('framereceived', ({ payload }) => frames.push(JSON.parse(String(payload))));
  });
  return async (predicate) => {
    const deadline = Date.now() + 10_000;
    while (!frames.some(predicate) && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
    assert.ok(frames.some(predicate), 'expected WebSocket frame was never received');
  };
}

test('two browser sessions exchange messages and recover the gap after a disconnect', {
  timeout: 100_000,
}, async (t) => {
  const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH });
  t.after(() => browser.close());
  const ownerContext = await browser.newContext();
  const memberContext = await browser.newContext();
  const owner = await ownerContext.newPage();
  const member = await memberContext.newPage();
  owner.setDefaultTimeout(12_000);
  member.setDefaultTimeout(12_000);
  const errors = [];
  for (const page of [owner, member]) page.on('pageerror', (err) => errors.push(err.message));

  // Retain a handle to the real browser socket so the test can close it at a
  // repeatable point. Every request, frame and server response uses the stack.
  await member.addInitScript(() => {
    const NativeWebSocket = window.WebSocket;
    window.WebSocket = class extends NativeWebSocket {
      constructor(...args) {
        super(...args);
        window.disconnectChat = () => this.close(1000, 'network interruption test');
      }
    };
  });

  const suffix = randomUUID().slice(0, 8);
  const ownerFrame = observeFrames(owner);
  const memberFrame = observeFrames(member);
  await owner.goto(origin + '/signup');
  await signup(owner, 'Owner', `owner-${suffix}@example.test`, 'Chat test ' + suffix);
  await owner.getByRole('button', { name: 'Create channel' }).click();
  await owner.getByLabel('Name', { exact: true }).fill('general');
  await owner.locator('form').getByRole('button', { name: 'Create channel' }).click();
  const composer = owner.getByRole('textbox', { name: 'Message #general' });
  await composer.waitFor();
  await owner.getByText('connected', { exact: true }).waitFor();

  await owner.getByRole('button', { name: 'Invite to workspace' }).click();
  const invite = new URL(await owner.getByLabel('Share invite link').inputValue());
  assert.equal(invite.origin, origin);
  assert.equal(invite.pathname, '/signup');
  assert.ok(new URLSearchParams(invite.hash.slice(1)).has('invite'));
  await member.goto(invite.href);
  await signup(member, 'Member', `member-${suffix}@example.test`);
  await member.getByRole('button', { name: 'general', exact: true }).click();
  await member.getByRole('textbox', { name: 'Message #general' }).waitFor();
  await member.getByText('connected', { exact: true }).waitFor();
  await memberFrame((event) => event.type === 'subscribed');

  async function send(text) {
    await composer.fill(text);
    await composer.press('Enter');
    await owner.getByText(text, { exact: true }).waitFor();
  }

  const live = 'Live message ' + suffix;
  // Forward the first POST to the real API and drop only its response. A
  // retry from the composer must reuse the key and resolve to the same row.
  const keys = [];
  await owner.route('**/messages', async (route) => {
    const req = route.request();
    if (req.method() !== 'POST') return route.continue();
    keys.push(req.headers()['idempotency-key']);
    if (keys.length !== 1) return route.continue();
    const committed = await route.fetch();
    assert.equal(committed.status(), 201);
    await route.abort('failed');
  });
  await composer.fill(live);
  await composer.press('Enter');
  await owner.getByText(/Network error/).waitFor();
  await composer.press('Enter');
  await owner.waitForFunction(() => document.querySelector('textarea')?.value === '');
  await memberFrame((event) => event.type === 'message' && event.message?.body === live);
  await member.getByText(live, { exact: true }).waitFor();
  assert.equal(keys.length, 2);
  assert.ok(keys[0]);
  assert.equal(keys[1], keys[0]);
  await owner.unroute('**/messages');

  const missed = 'Sent while disconnected ' + suffix;
  await memberContext.setOffline(true);
  await member.evaluate(() => window.disconnectChat());
  await member.getByText('disconnected', { exact: true }).waitFor();

  // Wait until Redis has delivered the outbox event to the healthy session.
  // The disconnected member can now recover it only from stored history.
  await send(missed);
  await ownerFrame((event) => event.type === 'message' && event.message?.body === missed);
  assert.equal(await member.getByText(missed, { exact: true }).count(), 0);

  const history = member.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.ok() && url.pathname.endsWith('/messages') && url.searchParams.has('after');
  });
  await memberContext.setOffline(false);
  await Promise.all([history, member.getByText(missed, { exact: true }).waitFor()]);
  await member.getByText('connected', { exact: true }).waitFor();
  for (const page of [owner, member]) {
    assert.equal(await page.getByText(live, { exact: true }).count(), 1);
    assert.equal(await page.getByText(missed, { exact: true }).count(), 1);
  }
  assert.deepEqual(errors, []);
});
