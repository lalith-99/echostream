// Runtime configuration read from Vite env vars (see .env.example).
// Falls back to local-dev defaults so `npm run dev` works with zero setup.

export const API_BASE_URL =
  import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8081';

export const WS_BASE_URL =
  import.meta.env.VITE_WS_BASE_URL ?? 'ws://localhost:8081';
