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

/** The active machine-store backend: OS secure store on desktop, else localStorage. */
export function machineStore(): MachineStore {
  if (!cached) {
    const bridge = getBridge();
    cached = bridge ? new DesktopMachineStore(bridge) : new LocalStorageMachineStore();
  }
  return cached;
}
