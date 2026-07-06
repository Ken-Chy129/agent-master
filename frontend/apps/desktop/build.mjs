// Bundles the Electron main and preload scripts into dist-electron/ with esbuild.
// CommonJS output so it can `require('electron')` and use ipcMain/contextBridge
// without ESM interop headaches in the Electron runtime.
import { build } from 'esbuild';
import { rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const outdir = resolve(here, 'dist-electron');

rmSync(outdir, { recursive: true, force: true });

/** @type {import('esbuild').BuildOptions} */
const common = {
  bundle: true,
  platform: 'node',
  target: 'node18',
  format: 'cjs',
  outdir,
  sourcemap: true,
  // Electron is provided by the runtime, never bundle it.
  external: ['electron'],
  logLevel: 'info',
};

await Promise.all([
  build({ ...common, entryPoints: [resolve(here, 'src/main.ts')] }),
  build({ ...common, entryPoints: [resolve(here, 'src/preload.ts')] }),
]);

console.log('[desktop] compiled main.ts + preload.ts -> dist-electron/');
