import { useCallback, useEffect, useRef, useState } from 'react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { echostream } from '../lib/api';
import { useAuthStore } from '../store/authStore';
import { useWebSocket } from '../hooks/useWebSocket';
import { Sidebar } from '../components/Sidebar';
import { MessagePane } from '../components/MessagePane';
import type { Channel, OutboundEvent } from '../lib/types';

export function ChatPage() {
  const token = useAuthStore((s) => s.token);
  return token ? <Session key={token} token={token} /> : null;
}

// A new login gets an independent cache; slow responses from a previous
// account cannot repopulate the current workspace's users or channels.
function Session({ token }: { token: string }) {
  const [client] = useState(() => new QueryClient({
    defaultOptions: { queries: { retry: 1, staleTime: 30_000 } },
  }));
  return <QueryClientProvider client={client}><Chat token={token} /></QueryClientProvider>;
}

function Chat({ token }: { token: string }) {
  const claims = useAuthStore((s) => s.claims);
  const logout = useAuthStore((s) => s.logout);
  const [channel, setChannel] = useState<Channel | null>(null);
  const [joinError, setJoinError] = useState('');
  const [choosing, setChoosing] = useState('');
  const selection = useRef(0);
  const listeners = useRef(new Set<(event: OutboundEvent) => void>());
  const listen = useCallback((handler: (event: OutboundEvent) => void) => {
    listeners.current.add(handler);
    return () => { listeners.current.delete(handler); };
  }, []);
  const emit = useCallback((event: OutboundEvent) => {
    listeners.current.forEach((handler) => handler(event));
  }, []);
  const { send, status } = useWebSocket(token, emit);
  const me = useQuery({ queryKey: ['me'], queryFn: echostream.me });

  useEffect(() => {
    if (!claims) return;
    const timer = setTimeout(logout, Math.max(0, claims.exp * 1000 - Date.now()));
    return () => clearTimeout(timer);
  }, [claims, logout]);

  async function choose(next: Channel) {
    const request = ++selection.current;
    setChoosing(next.name);
    setJoinError('');
    try {
      if (!next.is_private) await echostream.joinChannel(next.id);
      if (request === selection.current) setChannel(next);
    } catch (err) {
      if (request === selection.current) setJoinError(err instanceof Error ? err.message : 'Could not open channel.');
    } finally {
      if (request === selection.current) setChoosing('');
    }
  }

  return (
    <div className="flex h-full">
      <Sidebar activeChannelId={channel?.id} onSelectChannel={(ch) => void choose(ch)}
        onLogout={logout} userName={me.data?.display_name ?? '…'}
        userEmail={me.data?.email ?? ''} workspaceId={claims?.tenant_id ?? ''} />
      <div className="flex min-w-0 flex-1 flex-col">
        {choosing && <p role="status" className="px-4 py-2 text-xs">Opening #{choosing}…</p>}
        {joinError && <p role="alert" className="px-4 py-2 text-sm text-red-500">{joinError}</p>}
        <MessagePane channel={channel} wsStatus={status} send={send} onWsEvent={listen} />
      </div>
    </div>
  );
}
