import { useCallback, useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { echostream } from '../lib/api';
import { useAuthStore } from '../store/authStore';
import { useChatStore } from '../store/chatStore';
import { useWebSocket } from '../hooks/useWebSocket';
import { Sidebar } from '../components/Sidebar';
import { MessagePane } from '../components/MessagePane';
import type { Channel, Message, OutboundEvent } from '../lib/types';

export function ChatPage() {
  const token = useAuthStore((s) => s.token);
  const claims = useAuthStore((s) => s.claims);
  const logout = useAuthStore((s) => s.logout);
  const { activeChannel, setActiveChannel } = useChatStore();
  const queryClient = useQueryClient();

  const meQuery = useQuery({ queryKey: ['me'], queryFn: echostream.me });

  // Stable ref so the WS reconnect effect can read the current channel without
  // adding it to the effect dependency array (which would restart the connection).
  const activeChannelRef = useRef(activeChannel);
  useEffect(() => {
    activeChannelRef.current = activeChannel;
  }, [activeChannel]);

  // Listeners that MessagePane components register to receive WS events.
  const wsListeners = useRef<Set<(ev: OutboundEvent) => void>>(new Set());

  // Incoming WS message → prepend to TanStack Query cache for that channel.
  // Server returns newest-first; MessagePane's `select` reverses for display.
  const handleWsEvent = useCallback(
    (event: OutboundEvent) => {
      if (event.type === 'message' && event.message && event.channel_id) {
        queryClient.setQueryData<Message[]>(
          ['messages', event.channel_id],
          (prev) => [event.message!, ...(prev ?? [])],
        );
      }
      wsListeners.current.forEach((h) => h(event));
    },
    [queryClient],
  );

  // Stable subscribe function passed down to child components.
  const onWsEvent = useCallback((handler: (ev: OutboundEvent) => void) => {
    wsListeners.current.add(handler);
    return () => wsListeners.current.delete(handler);
  }, []);

  const { send, status: wsStatus } = useWebSocket(token, handleWsEvent);

  // Re-subscribe to the active channel whenever the WS reconnects.
  useEffect(() => {
    if (wsStatus === 'connected' && activeChannelRef.current) {
      send({ type: 'subscribe', channel_id: activeChannelRef.current.id });
    }
  }, [wsStatus, send]);

  const prevChannelId = useRef<string | null>(null);

  const handleSelectChannel = useCallback(
    async (channel: Channel) => {
      if (prevChannelId.current && prevChannelId.current !== channel.id) {
        send({ type: 'unsubscribe', channel_id: prevChannelId.current });
      }
      setActiveChannel(channel);
      prevChannelId.current = channel.id;
      // Invalidate so the message list always refetches from scratch on channel switch.
      queryClient.invalidateQueries({ queryKey: ['messages', channel.id] });
      try {
        await echostream.joinChannel(channel.id);
      } catch {
        // Idempotent — non-fatal if already a member.
      }
      send({ type: 'subscribe', channel_id: channel.id });
    },
    [send, setActiveChannel],
  );

  return (
    <div className="flex h-full">
      <Sidebar
        activeChannelId={activeChannel?.id}
        onSelectChannel={(ch) => void handleSelectChannel(ch)}
        onLogout={logout}
        userName={meQuery.data?.display_name ?? '…'}
        userEmail={meQuery.data?.email ?? ''}
        workspaceId={claims?.tenant_id ?? ''}
      />
      <MessagePane channel={activeChannel} wsStatus={wsStatus} send={send} onWsEvent={onWsEvent} />
    </div>
  );
}
