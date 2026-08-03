import { create } from 'zustand';

const TOKEN_KEY = 'echostream_token';

// Claims embedded in the EchoStream JWT (auth.GenerateToken).
export interface JwtClaims {
  user_id: string;
  tenant_id: string;
  email: string;
  exp: number;
}

function decodeJwt(token: string): JwtClaims | null {
  try {
    const payload = token.split('.')[1];
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
    return JSON.parse(json) as JwtClaims;
  } catch {
    return null;
  }
}

function isExpired(claims: JwtClaims | null): boolean {
  if (!claims) return true;
  return claims.exp * 1000 <= Date.now();
}

interface AuthState {
  token: string | null;
  claims: JwtClaims | null;
  setToken: (token: string) => void;
  logout: () => void;
  isAuthenticated: () => boolean;
}

function initialToken(): string | null {
  const token = localStorage.getItem(TOKEN_KEY);
  if (!token) return null;
  if (isExpired(decodeJwt(token))) {
    localStorage.removeItem(TOKEN_KEY);
    return null;
  }
  return token;
}

const bootToken = initialToken();

export const useAuthStore = create<AuthState>((set, get) => ({
  token: bootToken,
  claims: bootToken ? decodeJwt(bootToken) : null,
  setToken: (token) => {
    localStorage.setItem(TOKEN_KEY, token);
    set({ token, claims: decodeJwt(token) });
  },
  logout: () => {
    localStorage.removeItem(TOKEN_KEY);
    set({ token: null, claims: null });
  },
  isAuthenticated: () => {
    const { claims } = get();
    return !isExpired(claims);
  },
}));

// Read the raw token outside React (e.g. in the API client / WS connection).
export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}
