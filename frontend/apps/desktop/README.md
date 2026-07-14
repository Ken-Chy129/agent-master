# @agent-master/desktop

Electron desktop shell for agent-master. It wraps the shared React web app
(`@agent-master/web`) and adds two things the browser cannot provide:

- **OS-secure token storage** via Electron `safeStorage` (exposed to the web app
  as `window.agentMaster.secureStore`).
- **`agentmaster://` deep-link pairing** — `agent-master pair` prints an
  `agentmaster://pair?url=&token=&name=` link; opening it delivers the pairing
  payload to the running app.

The web app feature-detects `window.agentMaster` (the `DesktopBridge` contract
from `@agent-master/core`): when present it uses the secure store and deep-link
pairing; otherwise it falls back to `localStorage` and manual entry.

## Architecture

```
apps/desktop/
  build.mjs               esbuild bundler: src/*.ts -> dist-electron/*.js (CJS)
  package.json            @agent-master/desktop, main = dist-electron/main.js
  tsconfig.json           typecheck config for main/preload
  electron-builder.yml    packaging config (mac dmg, arm64+x64)
  src/
    main.ts               Electron main: window, secure-store IPC, deep links
    preload.ts            contextBridge -> window.agentMaster (DesktopBridge)
  dist-electron/          compiled output (git-ignored, produced by `compile`)
```

### `window.agentMaster` → IPC → main

| Bridge (renderer)                    | IPC channel             | Main process |
| ------------------------------------ | ----------------------- | ------------ |
| `secureStore.get(key)`               | `secure-store:get`      | read `userData/secure-store.json`, base64-decode + `safeStorage.decryptString` |
| `secureStore.set(key, value)`        | `secure-store:set`      | `safeStorage.encryptString` + base64, persist to JSON |
| `secureStore.delete(key)`            | `secure-store:delete`   | remove key from JSON |
| `onPair(cb)`                         | `agentmaster:pair` (main→renderer) | main forwards raw deep link; preload parses it (inlined `parsePairLink`) and calls `cb` |

`contextIsolation` is ON and `nodeIntegration` is OFF; all privileged work runs
in the main process. The preload inlines the `parsePairLink` semantics from
`packages/core/src/machines.ts` so it needs no runtime import of the ESM core
package (the `DesktopBridge`/`PairPayload` types are imported type-only).

If `safeStorage.isEncryptionAvailable()` is `false` (e.g. Linux/dev without a
keychain) values are persisted as base64 **plaintext** and a warning is logged.

## Develop

Two terminals (Vite dev server + Electron):

```bash
# 1) start the web dev server (http://localhost:5173)
npm run dev:web -w @agent-master/desktop
#    (equivalently: npm run dev -w @agent-master/web)

# 2) compile electron + launch it pointing at the dev URL
npm run dev -w @agent-master/desktop
```

`dev` sets `AM_DESKTOP_DEV=1`, which makes `main.ts` load
`http://localhost:5173` (override with `AM_DESKTOP_DEV_URL`). Without that env
the app loads the on-disk production web build.

## Build (no packaging)

```bash
npm run build -w @agent-master/desktop
```

This runs the web build (`@agent-master/web` → `apps/web/dist`) and compiles the
Electron main/preload into `dist-electron/`. To launch the built app against the
on-disk web build:

```bash
electron apps/desktop        # from the frontend/ root, with electron installed
```

## Package (macOS only)

electron-builder produces the distributables. **Run this on macOS** — a macOS
`.dmg` cannot be built on Linux.

```bash
npm run dist -w @agent-master/desktop     # dmg, arm64 + x64 -> release/
npm run pack:dir -w @agent-master/desktop # unpacked .app for quick testing
```

The web build must exist first; the `build`/`dist` scripts run it for you.
electron-builder copies `apps/web/dist` into the app as `web-dist/`, which
`main.ts` resolves at runtime.

### Electron download note

If the Electron binary or electron-builder's tooling downloads are slow or
blocked, set a mirror and retry:

```bash
export ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/
```

## Scripts

| Script      | What it does |
| ----------- | ------------ |
| `compile`   | esbuild `src/main.ts` + `src/preload.ts` → `dist-electron/` |
| `typecheck` | `tsc --noEmit` over `src/` |
| `dev`       | `AM_DESKTOP_DEV=1` compile + launch electron at the dev URL |
| `dev:web`   | run the Vite dev server for `@agent-master/web` |
| `build`     | web build + `compile` |
| `dist`      | `build` + electron-builder mac dmg arm64+x64 |
| `pack:dir`  | `build` + electron-builder `--dir` (unpacked) |
