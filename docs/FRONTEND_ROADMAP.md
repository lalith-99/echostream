# EchoStream Frontend & Portfolio Roadmap

> Goal: ship a real, production-grade web client for the EchoStream chat backend that
> (a) teaches me frontend end-to-end, (b) makes the repo obviously impressive to SDE2/SDE3
> recruiters, and (c) can be run as a **live demo** they can click through.
>
> This doc is the single source of truth for the frontend effort. We tackle **P0 first**,
> keep P1/P2 documented, and revisit later. Check items off as we go.

---

## 0. Guiding principles

- **Show, don't tell.** A recruiter should reach a live URL, click "Try demo", and be chatting
  in real time in under 10 seconds — no signup friction.
- **Evidence over claims.** README with architecture diagram, screenshots/GIFs, live demo link,
  green CI badge, and test coverage. Every feature should be demonstrable.
- **Production shape, not a toy.** Auth, protected routes, optimistic UI, reconnection, error
  boundaries, loading/empty/error states, accessibility, responsive layout, CI/CD, containerized.
- **Full-stack story.** The frontend proves I understand the backend I built (real-time fan-out,
  multi-tenancy, presence, cursor pagination) — that narrative is what separates SDE2 from SDE3.

---

## 1. Recommended tech stack

| Concern | Choice | Why |
|---|---|---|
| Framework | **React 18 + TypeScript** | Industry default; strong hiring signal; type-safety pairs with Go backend. |
| Build tool | **Vite** | Fast dev server, simple, modern. |
| Styling | **Tailwind CSS** | Rapid, consistent, production-common. |
| Server state | **TanStack Query** | Caching, retries, pagination, mutations — shows real data-fetching maturity. |
| Client state | **Zustand** | Lightweight store for auth/session/WS state. |
| Routing | **React Router v6** | Protected routes, nested layouts. |
| Realtime | Native **WebSocket** wrapper + reconnect/backoff | Directly exercises the backend WS protocol. |
| Forms/validation | **React Hook Form + Zod** | Type-safe validation, mirrors backend contracts. |
| UI primitives | **shadcn/ui** (Radix) | Accessible components; polished look with low effort. |
| Icons | **lucide-react** | Clean, consistent. |
| Testing | **Vitest** (unit) + **Playwright** (E2E) | Both are strong resume signals. |
| Lint/format | **ESLint + Prettier** | Table stakes. |
| Deploy (FE) | **Vercel** or **Netlify** | Free, instant, gives a shareable URL. |
| Deploy (BE) | **Fly.io** / **Render** / **Railway** | Free-tier Postgres + Redis + Go service for the live demo. |

> Alternative if I want to learn SSR/meta-framework: **Next.js (App Router)**. Heavier, but great
> resume signal. For a WS-heavy chat SPA, Vite+React is the leaner, more honest choice — recommend
> starting there and noting Next.js as a stretch.

---

## 2. Backend changes required (do these alongside the frontend)

These live in the Go repo, not the frontend. They unblock the client and improve the demo.

- [x] **P0 — CORS middleware.** Added `middleware.CORS` (`internal/middleware/cors.go`), wired
  globally in `cmd/server/main.go`, with allowlist from `CORS_ALLOWED_ORIGINS` env var
  (default `http://localhost:5173,http://localhost:3000`). Handles preflight OPTIONS, exposes
  rate-limit headers.
- [ ] **P0 — Health/readiness already exists** (`/v1/health`) — reuse for demo status badge.
- [ ] **P1 — Guest / demo login endpoint.** A `POST /v1/auth/demo` that provisions (or reuses) a
  seeded tenant + user and returns a JWT, so recruiters skip signup. Seed a couple of channels and
  a bot that posts messages.
- [ ] **P1 — Seed script.** Extend `scripts/` to create a demo tenant, channels, users, and a
  backfill of messages so the demo isn't an empty room.
- [ ] **P1 — List "my channels" vs "all channels".** Confirm channel list semantics for the
  sidebar (joined channels vs discoverable). May need a query param or new endpoint.
- [ ] **P2 — Message pagination metadata.** Ensure the cursor response exposes `has_more` / next
  cursor cleanly for infinite scroll.
- [ ] **P2 — User search / list for invites.** Invite flow needs a way to find users by email
  within a tenant.
- [ ] **P2 — Rate-limit headers.** Return `X-RateLimit-*` so the FE can surface throttling nicely.
- [ ] **P3 — Optionally serve the built SPA** from the Go binary (single-container demo) or keep
  FE/BE split. Document the tradeoff.

---

## 3. Feature backlog (prioritized)

### P0 — MVP: a working real-time chat client (build first)

- [x] Project scaffold: Vite + React + TS + Tailwind v4 + ESLint/Prettier, folder structure, env config (`web/`).
- [x] API client layer: typed `fetch` wrapper (`web/src/lib/apiClient.ts`), base URL from env, JWT injection, `ApiError` normalization, 401 auto-logout.
- [x] Auth: **Signup** page (tenant + user) and **Login** page → JWT stored in localStorage + Zustand, validated with React Hook Form + Zod, typed to backend contracts.
- [x] Protected routing: unauthenticated users redirect to `/login`; session hydrated from a non-expired JWT on reload.
- [x] App shell: sidebar (channel list) + main pane + footer (current user, tenant, logout). *(chat pane is a placeholder until M2)*
- [x] Channel list: fetch + render channels via TanStack Query. *(now filtered correctly — see bug fix below)*
- [x] Message history: cursor-paginated fetch, "Load older" button, ordered correctly.
- [x] Send message: input box, Enter-to-send, input cleared on success.
- [x] **WebSocket connection**: connect with JWT, `subscribe` to active channel, render incoming `message` frames live.
- [x] Loading / empty / error states for all surfaces.
- [x] Basic responsive layout (usable on mobile width).

### P1 — Real-time polish & collaboration

- [ ] **Typing indicators**: send `typing` on input; render "X is typing…" from WS frames.
- [ ] **Presence**: online/offline dots on members from `presence_change` frames + `/presence` endpoint.
- [x] Create channel (public/private) modal. *(sidebar + button)*
- [x] Join / leave channel. *(auto-join on channel select)*
- [ ] Members panel: list members, show presence.
- [x] Invite user to channel. *(backend `POST /v1/channels/:id/invite` exists)*
- [x] WebSocket **reconnect with backoff**; re-subscribe active channel on reconnect; connection status dot in header.
- [ ] Unread indicators / channel ordering by recent activity.
- [ ] Toast notifications for errors/success.
- [x] "Current user / tenant" surfaced in UI to demonstrate multi-tenancy.
- [x] Workspace invite link: `POST /v1/workspace/invite` → share URL → second user joins same workspace.
- [x] `GET /v1/users` — list all workspace users (for member names, future invite-by-email UI).

**Bug fixed:** Private channel visibility — `GET /v1/channels` now returns only public
channels + private channels where the caller is a member (was incorrectly returning all).

### P2 — Production hardening & polish

- [ ] Error boundaries + fallback UI.
- [ ] Dark mode toggle (persisted).
- [ ] Accessibility pass (keyboard nav, ARIA, focus management, color contrast).
- [ ] Skeleton loaders instead of spinners.
- [ ] Message timestamps with relative formatting + grouping by author/day.
- [ ] Rate-limit UX (surface 429 gracefully with retry hint).
- [ ] Empty-state illustrations / onboarding hints.
- [ ] Performance: virtualized message list for long histories.
- [ ] Config for prod API/WS URLs via env; no hardcoded localhost.

### P3 — Stretch / differentiators (great for SDE3 signal)

- [ ] Message reactions / emoji (would need a small backend addition — note as future).
- [ ] Threaded replies (backend addition — note as future).
- [ ] Markdown rendering + code blocks in messages.
- [ ] Link previews.
- [ ] File/image attachments (ties into the LocalStack/S3 bits already in the repo).
- [ ] Search messages.
- [ ] PWA / installable + push notifications.
- [ ] i18n scaffolding.

---

## 4. Portfolio / "recruiter-facing" deliverables

This is what actually converts the work into interviews. Treat as first-class, not afterthought.

- [ ] **Live demo URL** (FE on Vercel, BE on Fly/Render) + one-click **"Try demo"** guest login.
- [ ] **README upgrade**: hero screenshot/GIF of the app, live demo link, feature list, architecture
  diagram (reuse `docs/ARCHITECTURE.md`), tech stack, "how it works" (real-time fan-out story).
- [ ] **Screenshots / GIFs**: login → channel → live message arriving from a second browser tab.
- [ ] **CI badge**: GitHub Actions running lint + unit + build (+ E2E) on every PR.
- [ ] **Test evidence**: Vitest unit coverage + Playwright E2E recording of the core flow.
- [ ] **Architecture diagram for full stack** (FE ↔ REST/WS ↔ Go ↔ Postgres/Redis).
- [ ] **CHANGELOG / project journal** showing iterative, professional delivery.
- [ ] **Dockerized** frontend (multi-stage build → nginx) for reproducibility.
- [ ] **"Design decisions" section**: why Vite over Next, why optimistic UI, WS reconnection strategy,
  how multi-tenancy is reflected client-side. Recruiters love tradeoff reasoning.

---

## 5. Infra / DevOps (supports the live demo & CI evidence)

- [ ] Frontend Dockerfile (multi-stage: build → nginx serve).
- [ ] `docker-compose` update to optionally run FE alongside BE for local full-stack.
- [ ] GitHub Actions: FE pipeline (install → lint → typecheck → unit → build → E2E).
- [ ] Deploy pipelines: FE → Vercel, BE (+ managed Postgres/Redis) → Fly/Render.
- [ ] Environment/secrets management for demo (JWT secret, DB/Redis URLs) — never commit secrets.
- [ ] Uptime/health badge on README wired to `/v1/health`.

---

## 6. Suggested execution order (milestones)

1. **M0 — Unblock (BE):** CORS middleware + confirm channel/message/WS contracts. *(0.5–1 day)*
2. **M1 — Scaffold (FE):** Vite/React/TS/Tailwind, API client, auth pages, protected routes.
3. **M2 — Core chat:** channel list + message history + send + **live WS delivery**. *(the "wow")*
4. **M3 — Realtime polish:** typing, presence, reconnect, create/join/invite, members.
5. **M4 — Hardening:** error boundaries, dark mode, a11y, virtualization, rate-limit UX.
6. **M5 — Ship the demo:** deploy FE+BE, seed data, guest login, README + GIFs + CI badges.
7. **M6 — Stretch:** reactions/threads/markdown/attachments/search as time allows.

> **Definition of "portfolio-ready":** M0–M5 complete. A recruiter can open the live URL, click
> "Try demo", chat in real time across two tabs, and read a README that proves I understand the
> whole stack — with green CI and E2E evidence.

---

## 7. Open questions to resolve before/while building

- [x] Confirm exact JSON shapes for signup/login/message/channel responses — captured as TS types
  in `web/src/lib/types.ts` (and repo memory). **Gotcha:** send message uses field `content`,
  response/model uses `body`.
- [x] `GET /v1/channels` returns **all tenant channels** (offset pagination), NOT just joined ones.
  There is no "my channels" endpoint yet — sidebar currently shows all; a joined-filter is a P1 backend add.
- [x] Cursor pagination: messages response is a bare `Message[]` with **no `has_more`** — infer
  "more" when returned count == limit; cursor is `before=<message.id int64>`.
- [ ] Preferred hosting for the live BE demo (Fly.io vs Render vs Railway) — pick one for free Postgres+Redis.
- [ ] Keep FE/BE deployed separately, or serve the SPA from the Go binary for a single-container demo?
- [ ] **No bulk user lookup**: members/messages expose only `user_id`, no display name. Need a
  users-list endpoint to render names in the member list and message authors (P1/P2 backend add).

## 8. Known issues / decisions log

- **react-router-dom pinned to 7.18.2.** `npm audit` flags one high advisory (RSC-mode CSRF,
  GHSA-qwww-vcr4-c8h2). Every published v7 release is flagged by *some* advisory (ranges overlap
  the whole line); 7.18.2 is the latest and its only remaining advisory applies **exclusively to
  React Server Components / RSC mode**, which this client-only Vite SPA does not use. Revisit when a
  patched version (>8.2.0) ships.

---

_Last updated: 2026-08-02 — start with Section 2 (P0 CORS) and Section 3 (P0 MVP)._
