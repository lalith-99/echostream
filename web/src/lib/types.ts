// Types mirroring the EchoStream backend JSON contracts (internal/models,
// internal/api). Keep these in sync with the Go structs.

export interface User {
  id: string;
  tenant_id: string;
  email: string;
  display_name: string;
  created_at: string;
}

export interface Channel {
  id: string;
  tenant_id: string;
  name: string;
  is_private: boolean;
  created_at: string;
}

export interface Message {
  id: number; // int64 bigserial — used as the pagination cursor
  channel_id: string;
  sender_id: string;
  body: string;
  created_at: string;
}

export interface ChannelMember {
  channel_id: string;
  user_id: string;
  role: string;
}

export type PresenceStatus = 'online' | 'offline';

export interface MemberPresence {
  user_id: string;
  role: string;
  status: PresenceStatus;
}

export interface AuthResponse {
  token: string;
}

// --- WebSocket protocol (internal/websocket/message.go) ---

export type OutboundEventType =
  | 'message'
  | 'typing'
  | 'subscribed'
  | 'unsubscribed'
  | 'presence_change'
  | 'error';

export interface OutboundEvent {
  type: OutboundEventType;
  channel_id?: string;
  message?: Message;
  user_id?: string;
  status?: PresenceStatus;
  error?: string;
}

export type InboundEventType = 'subscribe' | 'unsubscribe' | 'typing';

export interface InboundEvent {
  type: InboundEventType;
  channel_id?: string;
  body?: string;
}
