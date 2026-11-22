// Production nginx serves both transports under /v1 on the web origin.
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ||
  (import.meta.env.DEV ? 'http://localhost:8081' : window.location.origin);

export const WS_BASE_URL = import.meta.env.VITE_WS_BASE_URL ||
  API_BASE_URL.replace(/^http/, 'ws');
