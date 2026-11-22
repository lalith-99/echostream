import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
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

  const queryClient = useQueryClient();
  const [invitee, setInvitee] = useState('');
  const invite = useMutation({
    mutationFn: (userId: string) => echostream.inviteToChannel(channelId, userId),
    onSuccess: () => {
      setInvitee('');
      void queryClient.invalidateQueries({ queryKey: ['presence', channelId] });
    },
  });
  const { data: members = [], isLoading, isError } = useQuery<MemberPresence[]>({
    queryKey: ['presence', channelId],
    queryFn: ({ signal }) => echostream.getChannelPresence(channelId, signal),
    enabled: open,
    refetchInterval: open ? 30_000 : false,
  });

  useEffect(() => onWsEvent((event) => {
    if (open && event.channel_id === channelId &&
        (event.type === 'presence_refresh' || event.type === 'subscribed')) {
      void queryClient.invalidateQueries({ queryKey: ['presence', channelId] });
    }
  }), [channelId, open, onWsEvent, queryClient]);
  const online = members.filter((member) => member.status === 'online');
  const offline = members.filter((member) => member.status === 'offline');
  const memberIds = new Set(members.map((member) => member.user_id));
  const candidates = Array.from(usersById.values()).filter((user) => !memberIds.has(user.id));

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
            {isLoading && <p className="px-2 text-xs text-slate-500">Loading members…</p>}
            {isError && <p role="alert" className="px-2 text-xs text-red-500">Could not load members.</p>}
            {!isLoading && !isError && members.length === 0 && (
              <p className="px-2 py-3 text-center text-xs text-slate-400">No members yet</p>
            )}
          </div>
          {!isLoading && !isError && candidates.length > 0 && (
            <form className="space-y-2 border-t border-slate-200 p-2 text-xs"
              onSubmit={(e) => { e.preventDefault(); if (invitee) invite.mutate(invitee); }}>
              <select aria-label="Workspace member" value={invitee}
                onChange={(e) => setInvitee(e.target.value)} className="w-full rounded border p-1 dark:bg-slate-900">
                <option value="">Select a workspace member</option>
                {candidates.map((u) => <option key={u.id} value={u.id}>{u.display_name}</option>)}
              </select>
              <button disabled={!invitee || invite.isPending} className="text-indigo-600 disabled:opacity-50">Add to channel</button>
              {invite.error && <p role="alert" className="text-red-500">{invite.error.message}</p>}
            </form>
          )}
        </div>
      )}
    </div>
  );
}
