# EchoStream Web

The React and TypeScript client uses REST for sign-in, channel membership and durable message history. A WebSocket carries live updates for the selected channel. The [main README](../README.md) describes the backend and deployment.

## Run the app

From the repository root, run `make up` and open `http://localhost:5173`. The web container proxies both REST and WebSocket requests to the API. Use two separate tabs to try two accounts. Create a workspace in the first tab, select **Invite to workspace**, and open the link in the second tab.

For frontend development, start the backend and its dependencies, then run these commands in `web/` with Node.js 22.13 or newer.

```sh
npm ci
npm run dev
```

Vite connects to `http://localhost:8081` by default. Production builds use the web origin and select `wss` on HTTPS. Set `VITE_API_BASE_URL` or `VITE_WS_BASE_URL` at build time for a separate API origin. Configure that origin's backend CORS and WebSocket allowlists for the browser's origin.

## Message delivery

- The client joins a public channel before it requests history or subscribes. A private channel appears for its members. The members panel can add another workspace member to the channel.
- Each draft gets a UUID for its send request. Retrying the same draft after a lost response reuses its key while that composer stays open.
- The saved HTTP response appears immediately. A duplicate live event or an overlapping history page resolves to the same message ID.
- On subscription, Redis resynchronization, browser network recovery and every 30 seconds, the active channel checks PostgreSQL-backed history. The forward cursor advances only through completed history reads. The live view retains 300 messages. Earlier history loads one 50-message page at a time and has a direct return to the live view.
- Workspace data and channel selection belong to the signed-in session. A new account starts with a fresh query cache. The tab stores the session token in session storage.

## Checks

| Command | What it covers |
|---|---|
| `npm test` | Duplicate and out-of-order delivery, multipage recovery, partial failure, bounded live history and channel cancellation |
| `npm run lint` | ESLint and React Hooks checks |
| `npm run build` | TypeScript validation and optimized assets in `dist/` |
