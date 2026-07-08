import {
  ApiClient,
  ApiError,
  SseClient,
  defaultMachineName,
  type CreateSessionRequest,
  type MachineProfile,
  type RecentSession,
  type RenderState,
  type WorkspaceListing,
} from '@agent-master/core';
import { create } from 'zustand';
import { getBridge, machineStore } from './storage.js';

/** Coarse SSE connection status for the currently-open session. */
export type StreamStatus = 'idle' | 'connecting' | 'open' | 'error';

const EMPTY_RENDER: RenderState = { basedOnSeq: 0, rows: [], tailActivity: 'idle' };

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

/**
 * A random UUID that also works outside secure contexts. `crypto.randomUUID`
 * is secure-context-only, and the desktop shell serves the UI over a
 * non-secure app:// origin (kept non-secure so plain-http daemon calls aren't
 * blocked as mixed content). `crypto.getRandomValues` is always available, so
 * fall back to a v4 UUID built from it.
 */
function randomId(): string {
  const c = globalThis.crypto;
  if (typeof c?.randomUUID === 'function') return c.randomUUID();
  const b = new Uint8Array(16);
  c.getRandomValues(b);
  b[6] = (b[6]! & 0x0f) | 0x40; // version 4
  b[8] = (b[8]! & 0x3f) | 0x80; // variant 10
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0'));
  return `${h.slice(0, 4).join('')}-${h.slice(4, 6).join('')}-${h.slice(6, 8).join('')}-${h.slice(8, 10).join('')}-${h.slice(10, 16).join('')}`;
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
  // Server-derived transcript per session (we dumb-render this).
  renderBySession: Record<string, RenderState>;

  streamStatus: StreamStatus;
  runActive: boolean;
  /** Live token-preview text for the current run; cleared when a committed snapshot lands. */
  streamingText: string;
  error: string | null;

  // lifecycle / machines
  init: () => Promise<void>;
  addMachine: (input: AddMachineInput) => Promise<void>;
  removeMachine: (id: string) => Promise<void>;
  selectMachine: (id: string) => Promise<void>;

  // sessions
  refreshSessions: () => Promise<void>;
  listWorkspaces: (path?: string) => Promise<WorkspaceListing | null>;
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
  renderBySession: {},
  streamStatus: 'idle',
  runActive: false,
  streamingText: '',
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
      id = randomId();
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
          renderBySession: {},
          streamStatus: 'idle',
          runActive: false,
          streamingText: '',
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
      renderBySession: {},
      streamStatus: 'idle',
      runActive: false,
      streamingText: '',
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

  listWorkspaces: async (path) => {
    const { api } = get();
    if (!api) return null;
    try {
      return await api.listWorkspaces(path);
    } catch (err) {
      set({ error: errText(err) });
      return null;
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
    const { sse } = get();
    if (!sse) return;

    stopStream();
    set({ currentSessionId: id, streamStatus: 'connecting', streamingText: '', error: null });

    // The stream's initial am_render snapshot provides the rows; no separate
    // history fetch needed. am_event only advances the SseClient resume cursor.
    activeUnsubscribe = sse.subscribe(id, {
      afterSeq: 0,
      onEvent: () => {},
      onRender: (rs) => {
        if (get().currentSessionId !== id) return;
        set((state) => ({
          renderBySession: { ...state.renderBySession, [id]: rs },
          runActive: rs.tailActivity === 'running',
          streamStatus: 'open',
          // A committed snapshot supersedes any in-flight token preview.
          streamingText: '',
        }));
      },
      onDelta: (delta) => {
        if (get().currentSessionId !== id) return;
        set((state) => ({ streamingText: state.streamingText + delta.text }));
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
    set({ currentSessionId: null, streamStatus: 'idle', runActive: false, streamingText: '' });
  },

  sendMessage: async (text) => {
    const { api, currentSessionId } = get();
    if (!api || !currentSessionId) return;
    const trimmed = text.trim();
    if (!trimmed) return;
    try {
      await api.send(currentSessionId, { message: trimmed, clientIntentId: randomId() });
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

export { EMPTY_RENDER };
