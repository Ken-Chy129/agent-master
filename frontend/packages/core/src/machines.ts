/**
 * Multi-machine model. A client (web/desktop/mobile) holds a list of machine
 * profiles — each is one agent-master daemon — and connects to the active one.
 * There is no central hub; the client talks to each daemon directly.
 */

/** One paired daemon. `token` is a secret; store it securely on desktop. */
export interface MachineProfile {
  id: string;
  name: string;
  baseUrl: string;
  token: string;
}

/** Persisted client state: the machine list plus which one is active. */
export interface PersistedMachines {
  machines: MachineProfile[];
  activeId: string | null;
}

/**
 * Storage adapter for the machine list. Web backs this with localStorage;
 * the desktop app backs it with the OS secure store via the DesktopBridge.
 */
export interface MachineStore {
  load(): Promise<PersistedMachines>;
  save(state: PersistedMachines): Promise<void>;
}

/**
 * Contract the Electron preload exposes on `window.agentMaster`. The web app
 * feature-detects it: when present it uses the OS-encrypted secure store and
 * listens for `agentmaster://pair` deep links; otherwise it falls back to
 * localStorage and manual entry.
 */
export interface DesktopBridge {
  readonly isDesktop: true;
  /** OS-encrypted key/value store (Electron safeStorage backed). */
  secureStore: {
    get(key: string): Promise<string | null>;
    set(key: string, value: string): Promise<void>;
    delete(key: string): Promise<void>;
  };
  /**
   * Subscribe to pairing deep links (`agentmaster://pair?url=&token=&name=`).
   * Returns an unsubscribe function.
   */
  onPair(cb: (payload: PairPayload) => void): () => void;
}

export interface PairPayload {
  url: string;
  token: string;
  name?: string;
}

/** The storage key used for the machine list in either backend. */
export const MACHINES_STORAGE_KEY = 'agent-master.machines';

/** Parse an `agentmaster://pair?...` deep link into a PairPayload, or null. */
export function parsePairLink(link: string): PairPayload | null {
  try {
    const u = new URL(link);
    if (u.protocol !== 'agentmaster:') return null;
    const url = u.searchParams.get('url');
    const token = u.searchParams.get('token');
    if (!url || !token) return null;
    const name = u.searchParams.get('name') ?? undefined;
    return { url, token, name };
  } catch {
    return null;
  }
}

/** Best-effort human label for a machine from its base URL (host[:port]). */
export function defaultMachineName(baseUrl: string): string {
  try {
    const u = new URL(baseUrl);
    return u.port ? `${u.hostname}:${u.port}` : u.hostname;
  } catch {
    return baseUrl;
  }
}
