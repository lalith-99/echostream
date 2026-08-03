import { useCallback, useEffect, useRef, useState } from 'react';
import { WS_BASE_URL } from '../lib/env';
import type { InboundEvent, OutboundEvent } from '../lib/types';

export type WSStatus = 'disconnected' | 'connecting' | 'connected';

// Manages one persistent WebSocket connection for the lifetime of the chat shell.
// Reconnects automatically with exponential backoff (1s → 2s → 4s … 30s cap).
export function useWebSocket(
  token: string | null,
  onEvent: (event: OutboundEvent) => void,
): { send: (msg: InboundEvent) => void; status: WSStatus } {
  const [status, setStatus] = useState<WSStatus>('disconnected');
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const attempt = useRef(0);
  // Ref lets onEvent change without restarting the connection.
  const onEventRef = useRef(onEvent);
  useEffect(() => {
    onEventRef.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    if (!token) return;

    // cancelled prevents state updates or reconnects after cleanup.
    let cancelled = false;

    function connect() {
      if (cancelled) return;
      setStatus('connecting');

      const ws = new WebSocket(`${WS_BASE_URL}/v1/ws?token=${token}`);
      wsRef.current = ws;

      ws.onopen = () => {
        if (cancelled) {
          ws.close();
          return;
        }
        attempt.current = 0;
        setStatus('connected');
      };

      ws.onmessage = (e: MessageEvent<string>) => {
        try {
          onEventRef.current(JSON.parse(e.data) as OutboundEvent);
        } catch {
          // Ignore malformed frames.
        }
      };

      const handleClose = () => {
        if (cancelled) return;
        wsRef.current = null;
        setStatus('disconnected');
        const delay = Math.min(1000 * 2 ** attempt.current, 30_000);
        attempt.current++;
        reconnectTimer.current = setTimeout(connect, delay);
      };
      ws.onclose = handleClose;
      // Closing triggers onclose which schedules the reconnect.
      ws.onerror = () => ws.close();
    }

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer.current !== null) clearTimeout(reconnectTimer.current);
      wsRef.current?.close();
      wsRef.current = null;
    };
  }, [token]);

  const send = useCallback((msg: InboundEvent) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(msg));
    }
  }, []);

  return { send, status };
}
