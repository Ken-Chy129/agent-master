import {
  MACHINES_STORAGE_KEY,
  type DesktopBridge,
  type MachineStore,
  type PersistedMachines,
} from '@agent-master/core';

const EMPTY: PersistedMachines = { machines: [], activeId: null };

/** Return the Electron desktop bridge if we're running inside the desktop app. */
export function getBridge(): DesktopBridge | null {
  const w = globalThis as unknown as { agentMaster?: DesktopBridge };
  return w.agentMaster?.isDesktop ? w.agentMaster : null;
}

function parse(raw: string | null): PersistedMachines {
  if (!raw) return EMPTY;
  try {
    const p = JSON.parse(raw) as Partial<PersistedMachines>;
    if (Array.isArray(p.machines)) {
      return { machines: p.machines, activeId: p.activeId ?? null };
    }
  } catch {
    // ignore malformed storage
  }
  return EMPTY;
}

class LocalStorageMachineStore implements MachineStore {
  async load(): Promise<PersistedMachines> {
    try {
      return parse(localStorage.getItem(MACHINES_STORAGE_KEY));
    } catch {
      return EMPTY;
    }
  }
  async save(state: PersistedMachines): Promise<void> {
    try {
      localStorage.setItem(MACHINES_STORAGE_KEY, JSON.stringify(state));
    } catch {
      // ignore quota / disabled storage
    }
  }
}

class DesktopMachineStore implements MachineStore {
  constructor(private readonly bridge: DesktopBridge) {}
  async load(): Promise<PersistedMachines> {
    return parse(await this.bridge.secureStore.get(MACHINES_STORAGE_KEY));
  }
  async save(state: PersistedMachines): Promise<void> {
    await this.bridge.secureStore.set(MACHINES_STORAGE_KEY, JSON.stringify(state));
  }
}

let cached: MachineStore | null = null;

const SEEN_KEY = 'agent-master.seenSeq';

/**
 * Per-session "last seen seq" marks, used to derive the needs-attention state
 * (agent replied and the user hasn't looked yet). Not a secret, so plain
 * localStorage is fine on both web and desktop.
 */
export function loadSeenSeq(): Record<string, number> {
  try {
    const raw = localStorage.getItem(SEEN_KEY);
    if (!raw) return {};
    const p = JSON.parse(raw) as unknown;
    if (p && typeof p === 'object') return p as Record<string, number>;
  } catch {
    // ignore malformed / disabled storage
  }
  return {};
}

export function saveSeenSeq(map: Record<string, number>): void {
  try {
    localStorage.setItem(SEEN_KEY, JSON.stringify(map));
  } catch {
    // ignore quota / disabled storage
  }
}

/** The active machine-store backend: OS secure store on desktop, else localStorage. */
export function machineStore(): MachineStore {
  if (!cached) {
    const bridge = getBridge();
    cached = bridge ? new DesktopMachineStore(bridge) : new LocalStorageMachineStore();
  }
  return cached;
}
