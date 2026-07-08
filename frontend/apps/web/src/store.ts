import {
  ApiClient,
  ApiError,
  SseClient,
  defaultMachineName,
  type CreateSessionRequest,
  type InfoResponse,
  type MachineProfile,
  type RecentSession,
  type RenderState,
  type Session,
  type WorkspaceListing,
} from '@agent-master/core';
import { create } from 'zustand';
import { getBridge, loadSeenSeq, machineStore, saveSeenSeq } from './storage.js';

/** Coarse SSE connection status for the currently-open session. */
export type StreamStatus = 'idle' | 'connecting' | 'open' | 'error';

/** Top-level view: the cross-machine overview or one machine's workspace. */
export type View = 'overview' | 'machine';

const EMPTY_RENDER: RenderState = { basedOnSeq: 0, rows: [], tailActivity: 'idle' };

/** How often the overview re-probes every machine (health + sessions). */
const POLL_INTERVAL_MS = 15_000;

/** Live per-machine data, fetched by polling. Never cleared on view switches. */
export interface MachineRuntime {
  /** null = not probed yet. */
  online: boolean | null;
  info: InfoResponse | null;
  sessions: RecentSession[];
  sessionsLoading: boolean;
}

const EMPTY_RUNTIME: MachineRuntime = {
  online: null,
  info: null,
  sessions: [],
  sessionsLoading: false,
};

// --- module-level (non-reactive) resources ---

/** ApiClient/SseClient per machine, keyed by id+url+token so edits invalidate. */
const clientCache = new Map<string, { api: ApiClient; sse: SseClient }>();

function clientsFor(m: MachineProfile): { api: ApiClient; sse: SseClient } {
  const key = `${m.id}|${m.baseUrl}|${m.token}`;
  let entry = clientCache.get(key);
  if (!entry) {
    const cfg = { baseUrl: m.baseUrl, token: m.token };
    entry = { api: new ApiClient(cfg), sse: new SseClient(cfg) };
    clientCache.set(key, entry);
  }
  return entry;
}

/** Unsubscribe for the active SSE subscription. */
let activeUnsubscribe: (() => void) | null = null;

function stopStream(): void {
  if (activeUnsubscribe) {
    activeUnsubscribe();
    activeUnsubscribe = null;
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null;

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
  /** Live data per machine id. */
  runtimes: Record<string, MachineRuntime>;
  /** Per-session last seen seq (persisted); drives the needs-attention state. */
  seenSeq: Record<string, number>;

  view: View;
  /** The machine whose workspace is open (view === 'machine'). */
  activeMachineId: string | null;

  /** The open session and which machine it lives on. */
  currentSessionId: string | null;
  currentSessionMachineId: string | null;
  /** Full session record (workspaceDir/model) for the conversation header. */
  currentSessionMeta: Session | null;

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

  // navigation
  openOverview: () => void;
  openMachine: (id: string) => void;

  // data refresh
  refreshMachine: (id: string) => Promise<void>;
  refreshAll: () => Promise<void>;

  // sessions
  listWorkspaces: (machineId: string, path?: string) => Promise<WorkspaceListing | null>;
  createSession: (machineId: string, req: CreateSessionRequest) => Promise<void>;
  openSession: (machineId: string, sessionId: string) => Promise<void>;
  closeSession: () => void;
  renameSession: (machineId: string, sessionId: string, title: string) => Promise<void>;
  deleteSession: (machineId: string, sessionId: string) => Promise<void>;
  sendMessage: (text: string) => Promise<void>;
  interrupt: () => Promise<void>;
  markSeen: (sessionId: string, seq: number) => void;
  clearError: () => void;
}

export const useStore = create<StoreState>((set, get) => {
  const machineById = (id: string | null): MachineProfile | null =>
    get().machines.find((m) => m.id === id) ?? null;

  const apiFor = (machineId: string): ApiClient | null => {
    const m = machineById(machineId);
    return m ? clientsFor(m).api : null;
  };

  const patchRuntime = (id: string, patch: Partial<MachineRuntime>) => {
    set((state) => ({
      runtimes: {
        ...state.runtimes,
        [id]: { ...(state.runtimes[id] ?? EMPTY_RUNTIME), ...patch },
      },
    }));
  };

  return {
    initialized: false,
    machines: [],
    runtimes: {},
    seenSeq: {},
    view: 'overview',
    activeMachineId: null,
    currentSessionId: null,
    currentSessionMachineId: null,
    currentSessionMeta: null,
    renderBySession: {},
    streamStatus: 'idle',
    runActive: false,
    streamingText: '',
    error: null,

    init: async () => {
      const persisted = await machineStore().load();
      set({
        initialized: true,
        machines: persisted.machines,
        seenSeq: loadSeenSeq(),
      });

      // Desktop: accept pairing deep links (agentmaster://pair?...).
      const bridge = getBridge();
      if (bridge) {
        bridge.onPair((p) => {
          void get().addMachine({ name: p.name, baseUrl: p.url, token: p.token });
        });
      }

      void get().refreshAll();
      if (!pollTimer) {
        pollTimer = setInterval(() => void get().refreshAll(), POLL_INTERVAL_MS);
      }
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
      await get().refreshMachine(id);
      get().openMachine(id);
    },

    removeMachine: async (id) => {
      const machines = get().machines.filter((m) => m.id !== id);
      set((state) => {
        const runtimes = { ...state.runtimes };
        delete runtimes[id];
        return { machines, runtimes };
      });
      await machineStore().save({ machines, activeId: machines[0]?.id ?? null });

      if (get().currentSessionMachineId === id) get().closeSession();
      if (get().activeMachineId === id) {
        set({ view: 'overview', activeMachineId: null });
      }
    },

    openOverview: () => {
      get().closeSession();
      set({ view: 'overview', activeMachineId: null, error: null });
      void get().refreshAll();
    },

    openMachine: (id) => {
      if (get().activeMachineId !== id) get().closeSession();
      set({ view: 'machine', activeMachineId: id, error: null });
      void get().refreshMachine(id);
    },

    refreshMachine: async (id) => {
      const api = apiFor(id);
      if (!api) return;
      patchRuntime(id, { sessionsLoading: true });
      try {
        const info = await api.info();
        const res = await api.listSessions(100, 0);
        patchRuntime(id, {
          online: true,
          info,
          sessions: res.sessions,
          sessionsLoading: false,
        });
      } catch {
        // Machine unreachable (or token rejected): keep the last session list
        // so the UI can still show recent context, just flag it offline.
        patchRuntime(id, { online: false, sessionsLoading: false });
      }
    },

    refreshAll: async () => {
      await Promise.allSettled(get().machines.map((m) => get().refreshMachine(m.id)));
    },

    listWorkspaces: async (machineId, path) => {
      const api = apiFor(machineId);
      if (!api) return null;
      try {
        return await api.listWorkspaces(path);
      } catch (err) {
        set({ error: errText(err) });
        return null;
      }
    },

    createSession: async (machineId, req) => {
      const api = apiFor(machineId);
      if (!api) return;
      try {
        const session = await api.createSession(req);
        await get().refreshMachine(machineId);
        await get().openSession(machineId, session.id);
      } catch (err) {
        set({ error: errText(err) });
      }
    },

    openSession: async (machineId, sessionId) => {
      const m = machineById(machineId);
      if (!m) return;
      const { api, sse } = clientsFor(m);

      stopStream();
      set({
        view: 'machine',
        activeMachineId: machineId,
        currentSessionId: sessionId,
        currentSessionMachineId: machineId,
        currentSessionMeta: null,
        streamStatus: 'connecting',
        streamingText: '',
        error: null,
      });

      // Mark as seen right away so the attention badge clears on open.
      const cached = get().runtimes[machineId]?.sessions.find((s) => s.id === sessionId);
      if (cached) get().markSeen(sessionId, cached.lastSeq);

      // Header metadata (workspaceDir/model); non-fatal if it fails.
      void api
        .getSession(sessionId)
        .then((meta) => {
          if (get().currentSessionId === sessionId) set({ currentSessionMeta: meta });
        })
        .catch(() => {});

      // The stream's initial am_render snapshot provides the rows; no separate
      // history fetch needed. am_event only advances the SseClient resume cursor.
      activeUnsubscribe = sse.subscribe(sessionId, {
        afterSeq: 0,
        onEvent: () => {},
        onRender: (rs) => {
          if (get().currentSessionId !== sessionId) return;
          set((state) => ({
            renderBySession: { ...state.renderBySession, [sessionId]: rs },
            runActive: rs.tailActivity === 'running',
            streamStatus: 'open',
            // A committed snapshot supersedes any in-flight token preview.
            streamingText: '',
          }));
          get().markSeen(sessionId, rs.basedOnSeq);
        },
        onDelta: (delta) => {
          if (get().currentSessionId !== sessionId) return;
          set((state) => ({ streamingText: state.streamingText + delta.text }));
        },
        onError: () => {
          if (get().currentSessionId === sessionId) set({ streamStatus: 'error' });
        },
        onReconnect: () => {
          if (get().currentSessionId === sessionId) set({ streamStatus: 'open' });
        },
      });
    },

    closeSession: () => {
      stopStream();
      set({
        currentSessionId: null,
        currentSessionMachineId: null,
        currentSessionMeta: null,
        streamStatus: 'idle',
        runActive: false,
        streamingText: '',
      });
    },

    renameSession: async (machineId, sessionId, title) => {
      const api = apiFor(machineId);
      if (!api) return;
      try {
        await api.renameSession(sessionId, title);
        await get().refreshMachine(machineId);
        if (get().currentSessionId === sessionId) {
          const meta = get().currentSessionMeta;
          if (meta) set({ currentSessionMeta: { ...meta, title } });
        }
      } catch (err) {
        set({ error: errText(err) });
      }
    },

    deleteSession: async (machineId, sessionId) => {
      const api = apiFor(machineId);
      if (!api) return;
      try {
        await api.deleteSession(sessionId);
        if (get().currentSessionId === sessionId) get().closeSession();
        set((state) => {
          const renderBySession = { ...state.renderBySession };
          delete renderBySession[sessionId];
          const seenSeq = { ...state.seenSeq };
          delete seenSeq[sessionId];
          saveSeenSeq(seenSeq);
          return { renderBySession, seenSeq };
        });
        await get().refreshMachine(machineId);
      } catch (err) {
        set({ error: errText(err) });
      }
    },

    sendMessage: async (text) => {
      const { currentSessionId, currentSessionMachineId } = get();
      if (!currentSessionId || !currentSessionMachineId) return;
      const api = apiFor(currentSessionMachineId);
      if (!api) return;
      const trimmed = text.trim();
      if (!trimmed) return;
      try {
        await api.send(currentSessionId, { message: trimmed, clientIntentId: randomId() });
      } catch (err) {
        set({ error: errText(err) });
      }
    },

    interrupt: async () => {
      const { currentSessionId, currentSessionMachineId } = get();
      if (!currentSessionId || !currentSessionMachineId) return;
      const api = apiFor(currentSessionMachineId);
      if (!api) return;
      try {
        await api.interrupt(currentSessionId);
      } catch (err) {
        set({ error: errText(err) });
      }
    },

    markSeen: (sessionId, seq) => {
      set((state) => {
        if ((state.seenSeq[sessionId] ?? 0) >= seq) return state;
        const seenSeq = { ...state.seenSeq, [sessionId]: seq };
        saveSeenSeq(seenSeq);
        return { seenSeq };
      });
    },

    clearError: () => set({ error: null }),
  };
});

export { EMPTY_RENDER, EMPTY_RUNTIME };
