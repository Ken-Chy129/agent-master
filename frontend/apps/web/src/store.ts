import {
  ApiClient,
  ApiError,
  SseClient,
  defaultMachineName,
  type CreateSessionRequest,
  type MachineProfile,
  type RecentSession,
  type WireEvent,
} from '@agent-master/core';
import { create } from 'zustand';
import { getBridge, machineStore } from './storage.js';

/** Coarse SSE connection status for the currently-open session. */
export type StreamStatus = 'idle' | 'connecting' | 'open' | 'error';

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

function makeClients(m: MachineProfile | null): { api: ApiClient | null; sse: SseClient | null } {
  if (!m) return { api: null, sse: null };
  const cfg = { baseUrl: m.baseUrl, token: m.token };
  return { api: new ApiClient(cfg), sse: new SseClient(cfg) };
}

function findMachine(machines: MachineProfile[], id: string | null): MachineProfile | null {
  return machines.find((m) => m.id === id) ?? null;
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

export interface AddMachineInput {
  name?: string;
  baseUrl: string;
  token: string;
}

interface StoreState {
  initialized: boolean;

  machines: MachineProfile[];
  activeMachineId: string | null;

  // Clients derived from the active machine.
  api: ApiClient | null;
  sse: SseClient | null;

  sessions: RecentSession[];
  sessionsLoading: boolean;

  currentSessionId: string | null;
  eventsBySession: Record<string, WireEvent[]>;
  historyLoading: boolean;

  streamStatus: StreamStatus;
  runActive: boolean;
  error: string | null;

  // lifecycle / machines
  init: () => Promise<void>;
  addMachine: (input: AddMachineInput) => Promise<void>;
  removeMachine: (id: string) => Promise<void>;
  selectMachine: (id: string) => Promise<void>;

  // sessions
  refreshSessions: () => Promise<void>;
  createSession: (req: CreateSessionRequest) => Promise<void>;
  openSession: (id: string) => Promise<void>;
  closeSession: () => void;
  sendMessage: (text: string) => Promise<void>;
  interrupt: () => Promise<void>;
  clearError: () => void;
}

/** Module-level unsubscribe for the active SSE subscription (not React state). */
let activeUnsubscribe: (() => void) | null = null;

function stopStream(): void {
  if (activeUnsubscribe) {
    activeUnsubscribe();
    activeUnsubscribe = null;
  }
}

export const useStore = create<StoreState>((set, get) => ({
  initialized: false,
  machines: [],
  activeMachineId: null,
  api: null,
  sse: null,

  sessions: [],
  sessionsLoading: false,
  currentSessionId: null,
  eventsBySession: {},
  historyLoading: false,
  streamStatus: 'idle',
  runActive: false,
  error: null,

  init: async () => {
    const persisted = await machineStore().load();
    let activeId = persisted.activeId;
    if (activeId && !persisted.machines.some((m) => m.id === activeId)) activeId = null;
    if (!activeId && persisted.machines.length > 0) activeId = persisted.machines[0]!.id;

    const { api, sse } = makeClients(findMachine(persisted.machines, activeId));
    set({
      initialized: true,
      machines: persisted.machines,
      activeMachineId: activeId,
      api,
      sse,
    });

    // Desktop: accept pairing deep links (agentmaster://pair?...).
    const bridge = getBridge();
    if (bridge) {
      bridge.onPair((p) => {
        void get().addMachine({ name: p.name, baseUrl: p.url, token: p.token });
      });
    }

    if (api) void get().refreshSessions();
  },

  addMachine: async (input) => {
    const cleanUrl = input.baseUrl.trim().replace(/\/+$/, '');
    const token = input.token.trim();
    const name = input.name?.trim() || defaultMachineName(cleanUrl);

    const existing = get().machines.find((m) => m.baseUrl === cleanUrl);
    let machines: MachineProfile[];
    let id: string;
    if (existing) {
      id = existing.id;
      machines = get().machines.map((m) => (m.id === id ? { ...m, name, token } : m));
    } else {
      id = crypto.randomUUID();
      machines = [...get().machines, { id, name, baseUrl: cleanUrl, token }];
    }

    set({ machines });
    await machineStore().save({ machines, activeId: id });
    await get().selectMachine(id);
  },

  removeMachine: async (id) => {
    const machines = get().machines.filter((m) => m.id !== id);
    const wasActive = get().activeMachineId === id;
    let nextActive = get().activeMachineId;
    if (wasActive) nextActive = machines[0]?.id ?? null;

    set({ machines });
    await machineStore().save({ machines, activeId: nextActive });

    if (wasActive) {
      if (nextActive) {
        await get().selectMachine(nextActive);
      } else {
        stopStream();
        set({
          activeMachineId: null,
          api: null,
          sse: null,
          sessions: [],
          currentSessionId: null,
          eventsBySession: {},
          streamStatus: 'idle',
          runActive: false,
        });
      }
    }
  },

  selectMachine: async (id) => {
    stopStream();
    const { api, sse } = makeClients(findMachine(get().machines, id));
    await machineStore().save({ machines: get().machines, activeId: id });
    set({
      activeMachineId: id,
      api,
      sse,
      sessions: [],
      currentSessionId: null,
      eventsBySession: {},
      streamStatus: 'idle',
      runActive: false,
      error: null,
    });
    if (api) void get().refreshSessions();
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
    set({ currentSessionId: id, historyLoading: true, streamStatus: 'connecting', error: null });

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

    const lastSeq = history.length > 0 ? history[history.length - 1]!.seq : 0;
    activeUnsubscribe = sse.subscribe(id, {
      afterSeq: lastSeq,
      onEvent: (event) => {
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
      await api.send(currentSessionId, { message: trimmed, clientIntentId: crypto.randomUUID() });
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
