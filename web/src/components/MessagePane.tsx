import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Hash, Lock, MessageSquare, Send } from 'lucide-react';
import { echostream } from '../lib/api';
import { useAuthStore } from '../store/authStore';
import { clsx } from 'clsx';
import { MembersPanel } from './MembersPanel';
import type { Channel, InboundEvent, Message, OutboundEvent, User } from '../lib/types';
import type { WSStatus } from '../hooks/useWebSocket';

// ─── helpers ────────────────────────────────────────────────────────────────

function formatTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(iso).toLocaleDateString();
}

function WSIndicator({ status }: { status: WSStatus }) {
  return (
    <span
      title={`WebSocket ${status}`}
      className={clsx('inline-block h-2 w-2 rounded-full', {
        'bg-emerald-400': status === 'connected',
        'bg-amber-400 animate-pulse': status === 'connecting',
        'bg-slate-400': status === 'disconnected',
      })}
    />
  );
}

// ─── Message bubble ──────────────────────────────────────────────────────────

function MessageBubble({
  msg,
  myId,
  usersById,
}: {
  msg: Message;
  myId: string;
  usersById: Map<string, User>;
}) {
  const isMe = msg.sender_id === myId;
  const user = usersById.get(msg.sender_id);
  const sender = isMe ? 'You' : (user?.display_name ?? msg.sender_id.slice(0, 8) + '…');

  return (
    <div className={clsx('flex flex-col gap-0.5 px-4 py-1', isMe && 'items-end')}>
      <span className="text-xs text-slate-400">
        <span className="font-medium text-slate-500 dark:text-slate-300">{sender}</span>
        {' · '}
        {formatTime(msg.created_at)}
      </span>
      <div
        className={clsx(
          'max-w-xs rounded-2xl px-4 py-2 text-sm break-words',
          isMe
            ? 'rounded-tr-sm bg-indigo-600 text-white'
            : 'rounded-tl-sm bg-slate-100 text-slate-900 dark:bg-slate-800 dark:text-slate-100',
        )}
      >
        {msg.body}
      </div>
    </div>
  );
}

// ─── Message input ──────────────────────────────────────────────────────────

function MessageInput({
  channel,
  onSend,
  onTyping,
}: {
  channel: Channel;
  onSend: (content: string) => Promise<void>;
  onTyping: () => void;
}) {
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit() {
    const content = text.trim();
    if (!content || sending) return;
    setSending(true);
    setError(null);
    try {
      await onSend(content);
      setText('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Send failed');
    } finally {
      setSending(false);
    }
  }

  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void submit();
      return;
    }
    // Notify the hub that this user is typing (best-effort, fire-and-forget).
    onTyping();
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    void submit();
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="border-t border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
    >
      {error && <p className="mb-1 text-xs text-red-500">{error}</p>}
      <div className="flex items-end gap-2">
        <textarea
          rows={1}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={`Message #${channel.name}`}
          className={clsx(
            'flex-1 resize-none rounded-xl border border-slate-200 px-4 py-2 text-sm outline-none transition',
            'bg-white text-slate-900 placeholder:text-slate-400',
            'focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/30',
            'dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100',
          )}
        />
        <button
          type="submit"
          disabled={!text.trim() || sending}
          className={clsx(
            'flex h-9 w-9 items-center justify-center rounded-xl transition',
            'bg-indigo-600 text-white hover:bg-indigo-500',
            'disabled:cursor-not-allowed disabled:opacity-50',
          )}
        >
          <Send className="h-4 w-4" />
        </button>
      </div>
      <p className="mt-1 text-right text-xs text-slate-400">
        Enter to send · Shift+Enter for new line
      </p>
    </form>
  );
}

// ─── Main pane ───────────────────────────────────────────────────────────────

interface Props {
  channel: Channel | null;
  wsStatus: WSStatus;
  send: (msg: InboundEvent) => void;
  onWsEvent: (handler: (ev: OutboundEvent) => void) => () => void;
}

// Props for the inner pane where channel is guaranteed non-null.
interface ActiveProps {
  channel: Channel;
  wsStatus: WSStatus;
  send: (msg: InboundEvent) => void;
  onWsEvent: (handler: (ev: OutboundEvent) => void) => () => void;
}

export function MessagePane({ channel, wsStatus, send, onWsEvent }: Props) {
  if (!channel) {
    return (
      <main className="flex flex-1 items-center justify-center bg-slate-50 dark:bg-slate-950">
        <div className="text-center">
          <MessageSquare className="mx-auto mb-3 h-10 w-10 text-slate-300" />
          <p className="text-slate-500">Select a channel to start chatting</p>
        </div>
      </main>
    );
  }

  return <ActivePane channel={channel} wsStatus={wsStatus} send={send} onWsEvent={onWsEvent} />;
}

// Separated so hooks only run when a channel IS selected.
function ActivePane({ channel, wsStatus, send, onWsEvent }: ActiveProps) {
  const queryClient = useQueryClient();
  const claims = useAuthStore((s) => s.claims);

  const { data: users = [] } = useQuery({
    queryKey: ['users'],
    queryFn: echostream.listUsers,
    staleTime: 5 * 60_000,
  });
  const usersById = new Map(users.map((u) => [u.id, u]));
  const bottomRef = useRef<HTMLDivElement>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);
  // user_ids currently typing in this channel (auto-cleared after 3s of silence).
  const [typingUsers, setTypingUsers] = useState<Map<string, string>>(new Map());
  const typingTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  // Subscribe to incoming WS events for this pane.
  useEffect(() => {
    const unsub = onWsEvent((ev) => {
      if (ev.type === 'typing' && ev.user_id && ev.user_id !== claims?.user_id) {
        const uid = ev.user_id;
        const name = usersById.get(uid)?.display_name ?? uid.slice(0, 8);
        setTypingUsers((prev) => new Map(prev).set(uid, name));
        // Clear after 3s of no new typing signal from this user.
        clearTimeout(typingTimers.current.get(uid));
        typingTimers.current.set(
          uid,
          setTimeout(() => {
            setTypingUsers((prev) => {
              const next = new Map(prev);
              next.delete(uid);
              return next;
            });
          }, 3000),
        );
      }
    });
    return unsub;
  }, [onWsEvent, claims?.user_id, usersById]);

  // Debounced typing signal sender — fires at most once per second of keystrokes.
  const typingTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const handleTyping = useCallback(() => {
    if (typingTimeout.current) return;
    send({ type: 'typing', channel_id: channel.id });
    typingTimeout.current = setTimeout(() => {
      typingTimeout.current = null;
    }, 1000);
  }, [send, channel.id]);

  // Fetch messages (server returns newest-first; select reverses for display).
  const { data: messages = [], isLoading, isError } = useQuery({
    queryKey: ['messages', channel.id],
    queryFn: () => echostream.listMessages(channel.id),
    select: (data) => [...data].reverse(),
  });

  // Scroll to newest message on initial load and when new messages arrive via WS.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length]);

  // Load up to 50 messages older than the oldest currently visible.
  async function handleLoadOlder() {
    const raw = queryClient.getQueryData<Message[]>(['messages', channel.id]) ?? [];
    // raw is newest-first; the oldest is at the end.
    const oldestId = raw[raw.length - 1]?.id;
    if (!oldestId) return;
    setLoadingOlder(true);
    try {
      const older = await echostream.listMessages(channel.id, oldestId);
      if (older.length > 0) {
        queryClient.setQueryData<Message[]>(['messages', channel.id], (prev = []) => [
          ...prev,  // current (newest-first)
          ...older, // even older (also newest-first within the page)
        ]);
      }
    } finally {
      setLoadingOlder(false);
    }
  }

  async function handleSend(content: string) {
    await echostream.sendMessage(channel.id, content);
    // Message arrives back via WebSocket — no need to add it manually here.
    // If WS is disconnected, it'll appear on next reconnect+refetch.
  }

  const mayHaveOlder = messages.length >= 50;

  return (
    <main className="flex flex-1 flex-col overflow-hidden bg-slate-50 dark:bg-slate-950">
      {/* Channel header */}
      <header className="flex items-center justify-between border-b border-slate-200 bg-white px-4 py-3 dark:border-slate-800 dark:bg-slate-900">
        <div className="flex items-center gap-2">
          {channel.is_private ? (
            <Lock className="h-4 w-4 text-slate-400" />
          ) : (
            <Hash className="h-4 w-4 text-slate-400" />
          )}
          <span className="font-semibold text-slate-900 dark:text-white">
            {channel.name}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <WSIndicator status={wsStatus} />
          <span className="text-xs text-slate-400">{wsStatus}</span>
          <MembersPanel
            channelId={channel.id}
            usersById={usersById}
            myUserId={claims?.user_id ?? ''}
            onWsEvent={onWsEvent}
          />
        </div>
      </header>

      {/* Message list */}
      <div className="flex-1 overflow-y-auto py-2">
        {isLoading && (
          <p className="px-4 py-8 text-center text-sm text-slate-400">Loading messages…</p>
        )}
        {isError && messages.length === 0 && (
          <p className="px-4 py-8 text-center text-sm text-red-500">Failed to load messages</p>
        )}

        {!isLoading && mayHaveOlder && (
          <div className="flex justify-center py-2">
            <button
              onClick={() => void handleLoadOlder()}
              disabled={loadingOlder}
              className="rounded-full border border-slate-200 bg-white px-4 py-1 text-xs text-slate-500 transition hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400"
            >
              {loadingOlder ? 'Loading…' : '↑ Load older messages'}
            </button>
          </div>
        )}

        {!isLoading && messages.length === 0 && (
          <p className="px-4 py-8 text-center text-sm text-slate-400">
            No messages yet. Say something!
          </p>
        )}

        {messages.map((msg) => (
          <MessageBubble key={msg.id} msg={msg} myId={claims?.user_id ?? ''} usersById={usersById} />
        ))}
        <div ref={bottomRef} />
      </div>

      {/* Typing indicator strip */}
      {typingUsers.size > 0 && (
        <div className="px-4 pb-1 text-xs text-slate-400 italic">
          {[...typingUsers.values()].join(', ')}
          {typingUsers.size === 1 ? ' is' : ' are'} typing…
        </div>
      )}

      <MessageInput channel={channel} onSend={handleSend} onTyping={handleTyping} />
    </main>
  );
}
