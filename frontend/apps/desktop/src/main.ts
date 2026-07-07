/**
 * Electron main process for the agent-master desktop shell.
 *
 * Responsibilities:
 *  - Create the BrowserWindow that loads the shared @agent-master/web UI
 *    (dev: http://localhost:5173, prod: apps/web/dist/index.html on disk).
 *  - Provide an OS-encrypted secure key/value store over IPC, backed by
 *    Electron `safeStorage` and persisted to `userData/secure-store.json`.
 *  - Register + handle the `agentmaster://` protocol and forward pairing
 *    deep links to the focused renderer as `agentmaster:pair`.
 *  - Enforce a single-instance lock so deep links funnel into one window.
 */
import { app, BrowserWindow, ipcMain, protocol, safeStorage, shell } from 'electron';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { dirname, extname, join, normalize, sep } from 'node:path';

const PROTOCOL = 'agentmaster';
// Custom scheme used to serve the packaged web UI (instead of file://). A
// standard+secure scheme lets Vite's ES-module scripts and assets load without
// the file:// CORS/MIME restrictions that otherwise leave a blank window.
const APP_SCHEME = 'app';
const isDev = process.env.AM_DESKTOP_DEV === '1';
const DEV_URL = process.env.AM_DESKTOP_DEV_URL ?? 'http://localhost:5173';

// `standard` gives the scheme a real tuple origin so same-origin module scripts
// and assets load cleanly. Deliberately NOT `secure`: a secure context would
// treat the plain-http daemon API/SSE calls as blocked mixed content. Must be
// registered before the app is ready.
protocol.registerSchemesAsPrivileged([
  { scheme: APP_SCHEME, privileges: { standard: true, supportFetchAPI: true } },
]);

let mainWindow: BrowserWindow | null = null;

// -------------------------------------------------------------------------
// Secure store (safeStorage-backed, persisted as base64 per key).
// -------------------------------------------------------------------------

type SecureStoreFile = Record<string, string>;

function secureStorePath(): string {
  return join(app.getPath('userData'), 'secure-store.json');
}

function readStoreFile(): SecureStoreFile {
  const path = secureStorePath();
  try {
    if (!existsSync(path)) return {};
    const raw = readFileSync(path, 'utf8');
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === 'object') return parsed as SecureStoreFile;
    return {};
  } catch (err) {
    console.warn('[desktop] failed to read secure-store.json, starting empty:', err);
    return {};
  }
}

function writeStoreFile(data: SecureStoreFile): void {
  const path = secureStorePath();
  try {
    mkdirSync(app.getPath('userData'), { recursive: true });
    writeFileSync(path, JSON.stringify(data), 'utf8');
  } catch (err) {
    console.error('[desktop] failed to write secure-store.json:', err);
    throw err;
  }
}

/**
 * Warn once (per process) if the OS keychain is unavailable. On dev/Linux
 * without a configured keychain we fall back to base64 plaintext so the app
 * still works, but the stored token is NOT OS-encrypted.
 */
let warnedNoEncryption = false;
function encryptionAvailable(): boolean {
  const available = safeStorage.isEncryptionAvailable();
  if (!available && !warnedNoEncryption) {
    warnedNoEncryption = true;
    console.warn(
      '[desktop] safeStorage encryption is NOT available; secure-store values ' +
        'will be persisted as base64 plaintext (no OS encryption). This is ' +
        'expected on some Linux/dev setups without a keychain.',
    );
  }
  return available;
}

function encodeValue(value: string): string {
  if (encryptionAvailable()) {
    return safeStorage.encryptString(value).toString('base64');
  }
  return Buffer.from(value, 'utf8').toString('base64');
}

function decodeValue(encoded: string): string {
  const buf = Buffer.from(encoded, 'base64');
  if (encryptionAvailable()) {
    try {
      return safeStorage.decryptString(buf);
    } catch (err) {
      // Value was likely written as plaintext before a keychain existed.
      console.warn('[desktop] decryptString failed; treating value as plaintext:', err);
      return buf.toString('utf8');
    }
  }
  return buf.toString('utf8');
}

function registerSecureStoreIpc(): void {
  ipcMain.handle('secure-store:get', (_evt, key: string): string | null => {
    const store = readStoreFile();
    const encoded = store[key];
    if (encoded === undefined) return null;
    try {
      return decodeValue(encoded);
    } catch (err) {
      console.error(`[desktop] secure-store get failed for key "${key}":`, err);
      return null;
    }
  });

  ipcMain.handle('secure-store:set', (_evt, key: string, value: string): void => {
    const store = readStoreFile();
    store[key] = encodeValue(value);
    writeStoreFile(store);
  });

  ipcMain.handle('secure-store:delete', (_evt, key: string): void => {
    const store = readStoreFile();
    if (key in store) {
      delete store[key];
      writeStoreFile(store);
    }
  });
}

// -------------------------------------------------------------------------
// Deep-link pairing.
// -------------------------------------------------------------------------

/** Pick the first `agentmaster://` argument out of a process argv array. */
function pairLinkFromArgv(argv: string[]): string | null {
  return argv.find((arg) => arg.startsWith(`${PROTOCOL}://`)) ?? null;
}

/** Forward a raw deep link to the renderer (focusing the window first). */
function forwardPairLink(link: string | null): void {
  if (!link) return;
  if (!mainWindow) return;
  if (mainWindow.isMinimized()) mainWindow.restore();
  mainWindow.focus();
  mainWindow.webContents.send('agentmaster:pair', link);
}

// -------------------------------------------------------------------------
// Window lifecycle.
// -------------------------------------------------------------------------

function resolveProdIndex(): string {
  // Two supported layouts, tried in order:
  //  - Packaged (electron-builder copies apps/web/dist -> <asar>/web-dist):
  //      <asar>/dist-electron/main.js  +  <asar>/web-dist/index.html
  //  - Unpackaged dev tree (running compiled main directly from the repo):
  //      apps/desktop/dist-electron/main.js  +  apps/web/dist/index.html
  const candidates = [
    join(__dirname, '..', 'web-dist', 'index.html'),
    join(__dirname, '..', '..', 'web', 'dist', 'index.html'),
  ];
  const found = candidates.find((p) => existsSync(p));
  if (!found) {
    console.error(
      '[desktop] no web build found. Run `npm run build -w @agent-master/web` first. Looked in:',
      candidates,
    );
  }
  return found ?? candidates[0]!;
}

const MIME_TYPES: Record<string, string> = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.mjs': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.map': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
};

/**
 * Serve the packaged web build over app://. Resolving paths against the web
 * root (with a traversal guard) and setting a correct Content-Type keeps
 * module scripts and assets loadable, which file:// does not.
 */
function registerAppProtocol(): void {
  const root = dirname(resolveProdIndex());
  protocol.handle(APP_SCHEME, async (request) => {
    const { pathname } = new URL(request.url);
    let rel = decodeURIComponent(pathname);
    if (rel === '/' || rel === '') rel = '/index.html';
    const filePath = normalize(join(root, rel));
    if (filePath !== root && !filePath.startsWith(root + sep)) {
      return new Response('Forbidden', { status: 403 });
    }
    try {
      const body = await readFile(filePath);
      const type = MIME_TYPES[extname(filePath).toLowerCase()] ?? 'application/octet-stream';
      return new Response(body, { headers: { 'Content-Type': type } });
    } catch {
      return new Response('Not found', { status: 404 });
    }
  });
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 720,
    minHeight: 480,
    title: 'agent-master',
    webPreferences: {
      preload: join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  if (isDev) {
    void mainWindow.loadURL(DEV_URL);
    mainWindow.webContents.openDevTools({ mode: 'detach' });
  } else {
    void mainWindow.loadURL(`${APP_SCHEME}://bundle/index.html`);
  }

  // Open external (http/https) links in the OS browser, not new Electron windows.
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith('http://') || url.startsWith('https://')) {
      void shell.openExternal(url);
      return { action: 'deny' };
    }
    return { action: 'deny' };
  });

  mainWindow.on('closed', () => {
    mainWindow = null;
  });
}

// -------------------------------------------------------------------------
// App bootstrap.
// -------------------------------------------------------------------------

// Register as the default handler for agentmaster:// links.
if (process.defaultApp && process.argv.length >= 2) {
  // When launched via `electron .` in dev, argv[1] is the app path; pass it so
  // the OS knows how to relaunch us for a deep link.
  app.setAsDefaultProtocolClient(PROTOCOL, process.execPath, [process.argv[1]!]);
} else {
  app.setAsDefaultProtocolClient(PROTOCOL);
}

const gotLock = app.requestSingleInstanceLock();
if (!gotLock) {
  app.quit();
} else {
  // Windows/Linux: a second launch (often carrying the deep link in argv) is
  // routed here into the primary instance.
  app.on('second-instance', (_evt, argv) => {
    forwardPairLink(pairLinkFromArgv(argv));
    if (mainWindow) {
      if (mainWindow.isMinimized()) mainWindow.restore();
      mainWindow.focus();
    }
  });

  // macOS: deep links arrive as an open-url event.
  app.on('open-url', (evt, url) => {
    evt.preventDefault();
    forwardPairLink(url);
  });

  app.whenReady().then(() => {
    registerSecureStoreIpc();
    if (!isDev) registerAppProtocol();
    createWindow();

    // Deliver a deep link supplied on the very first launch (Windows/Linux).
    const initialLink = pairLinkFromArgv(process.argv);
    if (initialLink) {
      // Renderer may not be ready yet; deliver once it finishes loading.
      mainWindow?.webContents.once('did-finish-load', () => forwardPairLink(initialLink));
    }

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow();
    });
  });

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit();
  });
}
