import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Hash, Lock, LogOut, MessageSquare, Plus } from 'lucide-react';
import { echostream } from '../lib/api';
import type { Channel } from '../lib/types';
import { CreateChannelModal } from './CreateChannelModal';
import { clsx } from 'clsx';

interface Props {
  activeChannelId: string | null | undefined;
  onSelectChannel: (channel: Channel) => void;
  onLogout: () => void;
  userName: string;
  userEmail: string;
  workspaceId: string;
}

export function Sidebar({
  activeChannelId,
  onSelectChannel,
  onLogout,
  userName,
  userEmail,
  workspaceId,
}: Props) {
  const [showCreate, setShowCreate] = useState(false);

  const channelsQuery = useQuery({
    queryKey: ['channels'],
    queryFn: () => echostream.listChannels(),
  });

  function handleCreated(channel: Channel) {
    setShowCreate(false);
    onSelectChannel(channel);
  }

  return (
    <>
      <aside className="flex w-64 flex-shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        {/* Workspace header */}
        <div className="flex items-center gap-2 border-b border-slate-200 px-4 py-4 dark:border-slate-800">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-indigo-600 text-white">
            <MessageSquare className="h-4 w-4" />
          </div>
          <span
            className="truncate font-semibold text-slate-900 dark:text-white"
            title={workspaceId}
          >
            Workspace {workspaceId.slice(0, 8)}
          </span>
        </div>

        {/* Channel list */}
        <div className="flex-1 overflow-y-auto px-2 py-3">
          <div className="mb-1 flex items-center justify-between px-2">
            <span className="text-xs font-semibold uppercase tracking-wide text-slate-400">
              Channels
            </span>
            <button
              onClick={() => setShowCreate(true)}
              title="Create channel"
              className="rounded p-0.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>

          {channelsQuery.isLoading && (
            <p className="px-2 text-sm text-slate-400">Loading…</p>
          )}
          {channelsQuery.isError && (
            <p className="px-2 text-sm text-red-500">Failed to load</p>
          )}
          {channelsQuery.data?.length === 0 && (
            <p className="px-2 text-sm text-slate-400">
              No channels yet —{' '}
              <button
                onClick={() => setShowCreate(true)}
                className="text-indigo-500 hover:underline"
              >
                create one
              </button>
            </p>
          )}

          <ul className="mt-1 space-y-0.5">
            {channelsQuery.data?.map((ch) => (
              <li key={ch.id}>
                <button
                  onClick={() => onSelectChannel(ch)}
                  className={clsx(
                    'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition',
                    activeChannelId === ch.id
                      ? 'bg-indigo-50 font-medium text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300'
                      : 'text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800',
                  )}
                >
                  {ch.is_private ? (
                    <Lock className="h-3.5 w-3.5 flex-shrink-0 text-slate-400" />
                  ) : (
                    <Hash className="h-3.5 w-3.5 flex-shrink-0 text-slate-400" />
                  )}
                  <span className="truncate">{ch.name}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>

        {/* User footer */}
        <div className="flex items-center justify-between border-t border-slate-200 px-3 py-3 dark:border-slate-800">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-slate-900 dark:text-white">
              {userName}
            </p>
            <p className="truncate text-xs text-slate-400">{userEmail}</p>
          </div>
          <button
            onClick={onLogout}
            title="Log out"
            className="rounded-md p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-slate-800"
          >
            <LogOut className="h-4 w-4" />
          </button>
        </div>
      </aside>

      {showCreate && (
        <CreateChannelModal
          onClose={() => setShowCreate(false)}
          onCreated={handleCreated}
        />
      )}
    </>
  );
}
