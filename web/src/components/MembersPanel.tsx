import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Users, X } from 'lucide-react';
import { echostream } from '../lib/api';
import type { MemberPresence, OutboundEvent, User } from '../lib/types';
import { clsx } from 'clsx';

function PresenceDot({ status }: { status: 'online' | 'offline' }) {
  return (
    <span
      title={status}
      className={clsx('inline-block h-2 w-2 flex-shrink-0 rounded-full', {
        'bg-emerald-400': status === 'online',
        'bg-slate-300 dark:bg-slate-600': status === 'offline',
      })}
    />
  );
}

interface Props {
  channelId: string;
  usersById: Map<string, User>;
  myUserId: string;
  onWsEvent: (handler: (ev: OutboundEvent) => void) => () => void;
}

export function MembersPanel({ channelId, usersById, myUserId, onWsEvent }: Props) {
  const [open, setOpen] = useState(false);

  const { data: members = [] } = useQuery<MemberPresence[]>({
    queryKey: ['presence', channelId],
    queryFn: () => echostream.getChannelPresence(channelId),
    enabled: open,
    refetchInterval: open ? 30_000 : false,
  });

  // Keep presence up-to-date from live WS events without a refetch.
  const [overrides, setOverrides] = useState<Map<string, 'online' | 'offline'>>(new Map());
  useEffect(() => {
    const unsub = onWsEvent((ev) => {
      if (ev.type === 'presence_change' && ev.user_id) {
        setOverrides((prev) =>
          new Map(prev).set(ev.user_id!, ev.status as 'online' | 'offline'),
        );
      }
    });
    return unsub;
  }, [onWsEvent]);

  const online = members.filter(
    (m) => (overrides.get(m.user_id) ?? m.status) === 'online',
  );
  const offline = members.filter(
    (m) => (overrides.get(m.user_id) ?? m.status) === 'offline',
  );

  function displayName(userId: string) {
    if (userId === myUserId) return 'You';
    return usersById.get(userId)?.display_name ?? userId.slice(0, 8) + '…';
  }

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        title="Members"
        className={clsx(
          'flex items-center gap-1.5 rounded-md px-2 py-1 text-xs transition',
          open
            ? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200'
            : 'text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800',
        )}
      >
        <Users className="h-3.5 w-3.5" />
        <span>{members.length || ''}</span>
      </button>

      {open && (
        <div className="absolute right-0 top-8 z-20 w-56 rounded-xl border border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-slate-900">
          <div className="flex items-center justify-between border-b border-slate-100 px-3 py-2 dark:border-slate-800">
            <span className="text-xs font-semibold text-slate-500 uppercase tracking-wide">
              Members
            </span>
            <button onClick={() => setOpen(false)} className="text-slate-400 hover:text-slate-600">
              <X className="h-3.5 w-3.5" />
            </button>
          </div>

          <div className="max-h-72 overflow-y-auto p-2">
            {online.length > 0 && (
              <>
                <p className="mb-1 px-2 text-xs font-medium text-slate-400">
                  Online — {online.length}
                </p>
                {online.map((m) => (
                  <div key={m.user_id} className="flex items-center gap-2 rounded-md px-2 py-1">
                    <PresenceDot status="online" />
                    <span className="truncate text-sm text-slate-800 dark:text-slate-100">
                      {displayName(m.user_id)}
                      {m.role === 'admin' && (
                        <span className="ml-1 text-xs text-slate-400">admin</span>
                      )}
                    </span>
                  </div>
                ))}
              </>
            )}
            {offline.length > 0 && (
              <>
                <p className="mb-1 mt-2 px-2 text-xs font-medium text-slate-400">
                  Offline — {offline.length}
                </p>
                {offline.map((m) => (
                  <div key={m.user_id} className="flex items-center gap-2 rounded-md px-2 py-1">
                    <PresenceDot status="offline" />
                    <span className="truncate text-sm text-slate-500">
                      {displayName(m.user_id)}
                    </span>
                  </div>
                ))}
              </>
            )}
            {members.length === 0 && (
              <p className="px-2 py-3 text-center text-xs text-slate-400">No members yet</p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
