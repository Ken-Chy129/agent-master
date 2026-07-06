/**
 * Preload script: exposes `window.agentMaster` implementing the core
 * `DesktopBridge` contract. Runs in an isolated context with Node access
 * limited to `ipcRenderer`; all privileged work happens in the main process.
 *
 * The `DesktopBridge` / `PairPayload` types are imported type-only for
 * compile-time conformance — types are erased at build time, so no ESM core
 * module is required at runtime.
 */
import { contextBridge, ipcRenderer } from 'electron';
import type { DesktopBridge, PairPayload } from '@agent-master/core';

/**
 * Inline copy of core `parsePairLink` semantics (machines.ts): protocol must be
 * `agentmaster:`, `url` and `token` query params are required, `name` optional.
 * Inlined so the preload has zero runtime dependency on the ESM core package.
 */
function parsePairLink(link: string): PairPayload | null {
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

const bridge: DesktopBridge = {
  isDesktop: true,
  secureStore: {
    get(key: string): Promise<string | null> {
      return ipcRenderer.invoke('secure-store:get', key);
    },
    set(key: string, value: string): Promise<void> {
      return ipcRenderer.invoke('secure-store:set', key, value);
    },
    delete(key: string): Promise<void> {
      return ipcRenderer.invoke('secure-store:delete', key);
    },
  },
  onPair(cb: (payload: PairPayload) => void): () => void {
    const listener = (_evt: Electron.IpcRendererEvent, link: string): void => {
      const payload = parsePairLink(link);
      if (payload) cb(payload);
    };
    ipcRenderer.on('agentmaster:pair', listener);
    return () => {
      ipcRenderer.removeListener('agentmaster:pair', listener);
    };
  },
};

contextBridge.exposeInMainWorld('agentMaster', bridge);
