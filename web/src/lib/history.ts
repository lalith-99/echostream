import type { Message } from './types';

export const PAGE_SIZE = 50;
export const LIVE_WINDOW = 300;
export interface HistoryCursor {
  before?: number;
  after?: number;
}

export function mergeMessages(current: Message[], incoming: Message[]): Message[] {
  if (!incoming.length) return current;
  const byId = new Map(current.map((message) => [message.id, message]));
  for (const message of incoming) byId.set(message.id, message);
  return Array.from(byId.values()).sort((a, b) => a.id - b.id);
}

export function mergeLiveMessages(current: Message[], incoming: Message[]): Message[] {
  const merged = mergeMessages(current, incoming);
  return merged.length > LIVE_WINDOW ? merged.slice(-LIVE_WINDOW) : merged;
}

// Only a successful history read advances this watermark. A live frame or HTTP
// acknowledgment may arrive ahead of an older message still waiting for fanout.
export async function reconcileHistory(
  cursor: number | null,
  read: (cursor: HistoryCursor) => Promise<Message[]>,
  apply: (page: Message[], watermark: number, initial: boolean) => void,
  signal: AbortSignal,
) {
  if (cursor === null) {
    const latest = await read({});
    if (!signal.aborted) apply(latest, latest[0]?.id ?? 0, true);
    return;
  }

  // Bound each pass so a busy channel yields to other work. The next pass
  // resumes after the last completed page, including after a transient error.
  for (let batch = 0; batch < 8 && !signal.aborted; batch++) {
    const newer = await read({ after: cursor });
    if (signal.aborted || newer.length === 0) return;
    cursor = newer[newer.length - 1].id;
    apply(newer, cursor, false);
    if (newer.length < PAGE_SIZE) return;
  }
}
