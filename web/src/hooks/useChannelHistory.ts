import { useCallback, useEffect, useRef, useState } from 'react';
import { echostream } from '../lib/api';
import { ApiError } from '../lib/apiClient';
import { LIVE_WINDOW, mergeLiveMessages, PAGE_SIZE, reconcileHistory } from '../lib/history';
import type { Message, OutboundEvent } from '../lib/types';

export type Listen = (handler: (event: OutboundEvent) => void) => () => void;

// Mounted with a channel key so state, cursors and outstanding work belong to
// exactly one channel. Confirmed messages from REST and WebSocket share IDs.
export function useChannelHistory(channelId: string, listen: Listen) {
  const [live, setLive] = useState<Message[]>([]);
  const [archive, setArchive] = useState<Message[] | null>(null);
  const [oldestKnown, setOldestKnown] = useState<number | null>(null);
  const messages = archive ?? live;
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [initialPageFull, setInitialPageFull] = useState(false);
  const hasOlder = messages.length > 0 && messages[0].id !== oldestKnown &&
    (archive !== null || initialPageFull || live.length === LIVE_WINDOW);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const after = useRef<number | null>(null);
  const lifetime = useRef<AbortController | null>(null);
  const readable = useRef(true);
  const add = useCallback((rows: Message[]) => {
    if (readable.current && lifetime.current && !lifetime.current.signal.aborted) {
      setLive((current) => mergeLiveMessages(current, rows));
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    let running = false;
    let requested = false;
    async function sync() {
      if (controller.signal.aborted) return;
      if (running) {
        requested = true;
        return;
      }
      running = true;
      try {
        do {
          requested = false;
          await reconcileHistory(after.current,
            (cursor) => echostream.listMessages(channelId, cursor, controller.signal),
            (page, watermark, initial) => {
              readable.current = true;
              add(page);
              after.current = watermark;
              if (initial) {
                setInitialPageFull(page.length === PAGE_SIZE);
                if (page.length < PAGE_SIZE) setOldestKnown(page.at(-1)?.id ?? null);
              }
            },
            controller.signal,
          );
        } while (requested && !controller.signal.aborted);
        if (!controller.signal.aborted) setError('');
      } catch (err) {
        if (!controller.signal.aborted) {
          if (err instanceof ApiError && (err.status === 403 || err.status === 404)) {
            readable.current = false;
            after.current = null;
            setLive([]);
            setArchive(null);
            setOldestKnown(null);
            setInitialPageFull(false);
          }
          setError(err instanceof Error ? err.message : 'Could not refresh messages.');
        }
      } finally {
        running = false;
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    const stop = listen((event) => {
      if (event.channel_id !== channelId) return;
      if (event.type === 'message' && event.message?.channel_id === channelId) {
        add([event.message]);
      } else if (event.type === 'subscribed' || event.type === 'resync') {
        void sync();
      }
    });
    void sync();
    const timer = setInterval(() => void sync(), 30_000);
    window.addEventListener('online', sync);
    return () => {
      stop();
      controller.abort();
      clearInterval(timer);
      window.removeEventListener('online', sync);
    };
  }, [add, channelId, listen]);

  async function navigate(direction: 'older' | 'newer') {
    const signal = lifetime.current?.signal;
    if (!signal || signal.aborted || !messages.length || loadingOlder) return;
    setLoadingOlder(true);
    try {
      const cursor = direction === 'older' ? { before: messages[0].id } : { after: messages[messages.length - 1].id };
      const page = await echostream.listMessages(channelId, cursor, signal);
      if (signal.aborted || !readable.current) return;
      if (direction === 'older') {
        if (page.length < PAGE_SIZE) setOldestKnown(page.at(-1)?.id ?? messages[0].id);
        if (page.length) setArchive([...page].reverse());
      } else if (!page.length || page[0].id >= (live[0]?.id ?? Infinity)) {
        setArchive(null);
      } else {
        setArchive(page);
      }
      setError('');
    } catch (err) {
      if (!signal.aborted) setError(err instanceof Error ? err.message : 'Could not load this history page.');
    } finally {
      if (!signal.aborted) setLoadingOlder(false);
    }
  }
  return {
    messages, loading, error, hasOlder, loadingOlder, navigate, add,
    browsingHistory: archive !== null, showLatest: () => setArchive(null),
  };
}
