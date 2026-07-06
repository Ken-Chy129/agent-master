import { fileURLToPath, URL } from 'node:url';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

/**
 * The daemon default port. The dev proxy below is only a fallback for when the
 * daemon's CORS headers aren't yet available: the ApiClient/SseClient are
 * always given an absolute baseUrl, which is preferred in dev and required in
 * production. To use the proxy, point the app's baseUrl at the Vite origin
 * (e.g. http://localhost:5173) so /api and /health resolve through it.
 */
const DAEMON_ORIGIN = process.env.AM_PROXY_TARGET ?? 'http://localhost:8888';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      // Resolve the workspace core package to its TS source for HMR + typing.
      '@agent-master/core': fileURLToPath(
        new URL('../../packages/core/src/index.ts', import.meta.url),
      ),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: DAEMON_ORIGIN,
        changeOrigin: true,
        ws: true, // proxy SSE/streaming
      },
      '/health': {
        target: DAEMON_ORIGIN,
        changeOrigin: true,
      },
    },
  },
});
