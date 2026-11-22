import { PAGE_SIZE, type HistoryCursor } from './history';
import { api } from './apiClient';
import type { AuthResponse, Channel, MemberPresence, Message, User } from './types';

export interface InviteResponse {
  token: string;
  invite_url: string;
  expires_at: string;
}

export interface SignupInput {
  email: string;
  password: string;
  display_name: string;
  tenant_name: string;
  invite_token?: string;
}

export interface LoginInput {
  email: string;
  password: string;
}

async function readDirectory<T>(
  path: string, limit: number, key: (row: T) => string, signal?: AbortSignal,
): Promise<T[]> {
  const results = new Map<string, T>();
  for (let offset = 0; ; offset += limit) {
    const rows = await api.get<T[]>(`${path}?limit=${limit}&offset=${offset}`, { signal });
    const previousCount = results.size;
    for (const row of rows) results.set(key(row), row);
    if (rows.length < limit || results.size === previousCount) return [...results.values()];
  }
}

export const echostream = {
  signup: (input: SignupInput) =>
    api.post<AuthResponse>('/v1/auth/signup', input, { auth: false }),

  login: (input: LoginInput) =>
    api.post<AuthResponse>('/v1/auth/login', input, { auth: false }),

  me: () => api.get<User>('/v1/users/me'),

  listUsers: (signal?: AbortSignal) =>
    readDirectory<User>('/v1/users', 200, (user) => user.id, signal),

  generateInvite: () => api.post<InviteResponse>('/v1/workspace/invite'),

  getChannelPresence: (channelId: string, signal?: AbortSignal) =>
    readDirectory<MemberPresence>(`/v1/channels/${channelId}/presence`, 1000, (row) => row.user_id, signal),

  listChannels: (limit = 50, offset = 0, signal?: AbortSignal) =>
    api.get<Channel[]>(`/v1/channels?limit=${limit}&offset=${offset}`, { signal }),

  createChannel: (name: string, isPrivate: boolean) =>
    api.post<Channel>('/v1/channels', { name, is_private: isPrivate }),

  listMessages: (channelId: string, cursor: HistoryCursor = {}, signal?: AbortSignal) => {
    const query = new URLSearchParams({ limit: String(PAGE_SIZE) });
    if (cursor.before !== undefined) query.set('before', String(cursor.before));
    if (cursor.after !== undefined) query.set('after', String(cursor.after));
    return api.get<Message[]>(`/v1/channels/${channelId}/messages?${query}`, { signal });
  },

  sendMessage: (channelId: string, content: string, key: string) =>
    api.post<Message>(`/v1/channels/${channelId}/messages`, { content }, {
      headers: { 'Idempotency-Key': key },
    }),

  inviteToChannel: (channelId: string, userId: string) =>
    api.post<void>(`/v1/channels/${channelId}/invite`, { user_id: userId }),

  joinChannel: (channelId: string) =>
    api.post<void>(`/v1/channels/${channelId}/join`, {}),
};
