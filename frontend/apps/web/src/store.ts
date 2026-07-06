import {
  ApiClient,
  ApiError,
  SseClient,
  type CreateSessionRequest,
  type RecentSession,
  type WireEvent,
} from '@agent-master/core';
import { create } from 'zustand';

const STORAGE_KEY = 'agent-master.connection';

export interface ConnectionConfig {
  baseUrl: string;
  token: string;
}

/** Coarse SSE connection status for the currently-open session. */
export type StreamStatus = 'idle' | 'connecting' | 'open' | 'error';

function loadConnection(): ConnectionConfig | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<ConnectionConfig>;
    if (parsed.baseUrl && parsed.token) {
      return { baseUrl: parsed.baseUrl, token: parsed.token };
    }
  } catch {
    // ignore malformed storage
  }
  return null;
}

function saveConnection(conn: ConnectionConfig | null): void {
  try {
    if (conn) localStorage.setItem(STORAGE_KEY, JSON.stringify(conn));
    else localStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore quota / disabled storage
  }
}

/** Insert/replace an event by seq and keep the list sorted ascending. */
function upsertEvent(list: WireEvent[], event: WireEvent): WireEvent[] {
  const idx = list.findIndex((e) => e.seq === event.seq);
  if (idx >= 0) {
    const next = list.slice();
    next[idx] = event;
    return next;
  }
  const next = [...list, event];
  next.sort((a, b) => a.seq - b.seq);
  return next;
}

interface StoreState {
  connection: ConnectionConfig | null;
  api: ApiClient | null;
  sse: SseClient | null;

  sessions: RecentSession[];
  sessionsLoading: boolean;

  currentSessionId: string | null;
  eventsBySession: Record<string, WireEvent[]>;
  historyLoading: boolean;

  streamStatus: StreamStatus;
  /** True while a run is active in the current session (drives Composer state). */
  runActive: boolean;

  /** Last surfaced error message for banner display. */
  error: string | null;

  // actions
  connect: (conn: ConnectionConfig) => void;
  disconnect: () => void;
  refreshSessions: () => Promise<void>;
  createSession: (req: CreateSessionRequest) => Promise<void>;
  openSession: (id: string) => Promise<void>;
  closeSession: () => void;
  sendMessage: (text: string) => Promise<void>;
  interrupt: () => Promise<void>;
  clearError: () => void;
}

/** Module-level unsubscribe for the active SSE subscription (not stored in React state). */
let activeUnsubscribe: (() => void) | null = null;

function stopStream(): void {
  if (activeUnsubscribe) {
    activeUnsubscribe();
    activeUnsubscribe = null;
  }
}

function errText(err: unknown): string {
  if (err instanceof ApiError) return `${err.message} (HTTP ${err.status})`;
  if (err instanceof Error) return err.message;
  return String(err);
}

/** Derive whether a run is active from the current event list (last run event wins). */
function computeRunActive(events: WireEvent[]): boolean {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    if (!e) continue;
    if (e.type === 'run_finished') return false;
    if (e.type === 'run_started') return true;
  }
  return false;
}

export const useStore = create<StoreState>((set, get) => ({
  connection: loadConnection(),
  api: (() => {
    const c = loadConnection();
    return c ? new ApiClient(c) : null;
  })(),
  sse: (() => {
    const c = loadConnection();
    return c ? new SseClient(c) : null;
  })(),

  sessions: [],
  sessionsLoading: false,
  currentSessionId: null,
  eventsBySession: {},
  historyLoading: false,
  streamStatus: 'idle',
  runActive: false,
  error: null,

  connect: (conn) => {
    saveConnection(conn);
    stopStream();
    set({
      connection: conn,
      api: new ApiClient(conn),
      sse: new SseClient(conn),
      sessions: [],
      currentSessionId: null,
      eventsBySession: {},
      streamStatus: 'idle',
      runActive: false,
      error: null,
    });
    void get().refreshSessions();
  },

  disconnect: () => {
    stopStream();
    saveConnection(null);
    set({
      connection: null,
      api: null,
      sse: null,
      sessions: [],
      currentSessionId: null,
      eventsBySession: {},
      streamStatus: 'idle',
      runActive: false,
      error: null,
    });
  },

  refreshSessions: async () => {
    const { api } = get();
    if (!api) return;
    set({ sessionsLoading: true });
    try {
      const res = await api.listSessions(100, 0);
      set({ sessions: res.sessions, sessionsLoading: false });
    } catch (err) {
      set({ sessionsLoading: false, error: errText(err) });
    }
  },

  createSession: async (req) => {
    const { api } = get();
    if (!api) return;
    try {
      const session = await api.createSession(req);
      await get().refreshSessions();
      await get().openSession(session.id);
    } catch (err) {
      set({ error: errText(err) });
    }
  },

  openSession: async (id) => {
    const { api, sse } = get();
    if (!api || !sse) return;

    stopStream();
    set({
      currentSessionId: id,
      historyLoading: true,
      streamStatus: 'connecting',
      error: null,
    });

    // 1) Load history (ascending by seq).
    let history: WireEvent[] = [];
    try {
      const res = await api.getMessages(id, { limit: 200 });
      history = res.events;
    } catch (err) {
      set({ historyLoading: false, streamStatus: 'error', error: errText(err) });
      return;
    }

    set((state) => ({
      historyLoading: false,
      eventsBySession: { ...state.eventsBySession, [id]: history },
      runActive: computeRunActive(history),
    }));

    // 2) Open the live stream from the last known seq.
    const lastSeq = history.length > 0 ? history[history.length - 1]!.seq : 0;
    activeUnsubscribe = sse.subscribe(id, {
      afterSeq: lastSeq,
      onEvent: (event) => {
        // Ignore events for a session that's no longer current.
        if (get().currentSessionId !== id) return;
        set((state) => {
          const list = upsertEvent(state.eventsBySession[id] ?? [], event);
          return {
            eventsBySession: { ...state.eventsBySession, [id]: list },
            runActive: computeRunActive(list),
          };
        });
      },
      onError: () => {
        if (get().currentSessionId === id) set({ streamStatus: 'error' });
      },
      onReconnect: () => {
        if (get().currentSessionId === id) set({ streamStatus: 'open' });
      },
    });
  },

  closeSession: () => {
    stopStream();
    set({ currentSessionId: null, streamStatus: 'idle', runActive: false });
  },

  sendMessage: async (text) => {
    const { api, currentSessionId } = get();
    if (!api || !currentSessionId) return;
    const trimmed = text.trim();
    if (!trimmed) return;
    try {
      await api.send(currentSessionId, {
        message: trimmed,
        clientIntentId: crypto.randomUUID(),
      });
      // The user_message + run_started events arrive over SSE; runActive flips there.
    } catch (err) {
      set({ error: errText(err) });
    }
  },

  interrupt: async () => {
    const { api, currentSessionId } = get();
    if (!api || !currentSessionId) return;
    try {
      await api.interrupt(currentSessionId);
    } catch (err) {
      set({ error: errText(err) });
    }
  },

  clearError: () => set({ error: null }),
}));
