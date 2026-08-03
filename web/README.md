# EchoStream Web

React + TypeScript frontend for the [EchoStream](../README.md) real-time chat backend.

## Tech stack

| Layer | Technology |
|---|---|
| Framework | React 19 + TypeScript |
| Build tool | Vite |
| Styling | Tailwind CSS v4 |
| Server state | TanStack Query |
| Client state | Zustand |
| Routing | React Router v7 |
| Forms | React Hook Form + Zod |
| Icons | lucide-react |

## Features

- **Auth** — signup (creates workspace) / login, JWT-backed session with auto-logout on expiry
- **Workspace invites** — generate an invite link; second user joins the same workspace without a separate signup tenant
- **Channel list** — public channels visible to all; private channels only shown to members
- **Real-time messaging** — WebSocket delivery with exponential-backoff reconnection and per-channel subscribe/unsubscribe
- **Message history** — cursor-paginated fetch with "Load older messages" support
- **WS status indicator** — live connection state (connecting / connected / disconnected) in the channel header

## Getting started

```bash
# 1. Install dependencies
npm install

# 2. Configure API endpoints (defaults point to the local backend on :8081)
cp .env.example .env

# 3. Start the dev server → http://localhost:5173
npm run dev
```

The backend must be running at `VITE_API_BASE_URL` (default `http://localhost:8081`). Start it from the repo root:

```bash
docker compose up -d && make run
```

## Scripts

| Command | Description |
|---|---|
| `npm run dev` | Dev server with hot-reload |
| `npm run build` | Typecheck (`tsc`) + production build → `dist/` |
| `npm run typecheck` | Type-check only |
| `npm run lint` | ESLint |
| `npm run format` | Prettier |

## Project structure

```
src/
  lib/          # API client, typed endpoint functions, env config, shared types
  store/        # Zustand stores (auth, active channel)
  hooks/        # useWebSocket — connection lifecycle + reconnect logic
  routes/       # Router config + ProtectedRoute guard
  pages/        # LoginPage, SignupPage, ChatPage
  components/   # Sidebar, MessagePane, CreateChannelModal, reusable UI
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `VITE_API_BASE_URL` | `http://localhost:8081` | EchoStream REST API base URL |
| `VITE_WS_BASE_URL` | `ws://localhost:8081` | EchoStream WebSocket base URL |

## Architecture

See [../README.md](../README.md) and [../docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md) for the full system design, including the real-time fan-out path (REST → Postgres → Redis pub/sub → WebSocket hub → browser).
