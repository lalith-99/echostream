import { api } from './apiClient';
import type { AuthResponse, Channel, Message, User } from './types';

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

export const echostream = {
  signup: (input: SignupInput) =>
    api.post<AuthResponse>('/v1/auth/signup', input, { auth: false }),

  login: (input: LoginInput) =>
    api.post<AuthResponse>('/v1/auth/login', input, { auth: false }),

  me: () => api.get<User>('/v1/users/me'),

  listUsers: () => api.get<User[]>('/v1/users'),

  generateInvite: () => api.post<InviteResponse>('/v1/workspace/invite'),

  listChannels: (limit = 50, offset = 0) =>
    api.get<Channel[]>(`/v1/channels?limit=${limit}&offset=${offset}`),

  createChannel: (name: string, isPrivate: boolean) =>
    api.post<Channel>('/v1/channels', { name, is_private: isPrivate }),

  // Returns up to 50 messages newest-first. Pass before to page backwards.
  listMessages: (channelId: string, before?: number) =>
    api.get<Message[]>(
      `/v1/channels/${channelId}/messages?limit=50${
        before !== undefined ? `&before=${before}` : ''
      }`,
    ),

  // NOTE: backend send field is `content`, but response/model field is `body`.
  sendMessage: (channelId: string, content: string) =>
    api.post<Message>(`/v1/channels/${channelId}/messages`, { content }),

  // Idempotent — safe to call every time a channel is selected.
  joinChannel: (channelId: string) =>
    api.post<void>(`/v1/channels/${channelId}/join`, { role: 'member' }),
};
