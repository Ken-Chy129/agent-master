#!/usr/bin/env node
/**
 * Integration smoke test against a REAL agent-master daemon.
 *
 * It exercises the same code paths the web app uses via @agent-master/core:
 *   1. create a session in a temp workspace dir
 *   2. subscribe to the SSE stream
 *   3. send a trivial message
 *   4. read events until run_finished, printing the assistant reply
 *
 * Usage:
 *   AM_BASE_URL=http://localhost:8899 AM_TOKEN=<token> node scripts/smoke.mjs
 *
 * Node 20 has global fetch but no EventSource, so we inject one from the
 * `eventsource` package (a devDependency, used for this script only).
 *
 * Node 20 cannot strip TS types, so this imports the compiled output. Run
 * `npm run build -w @agent-master/core` first (the `npm run smoke` script and
 * the top-level check do this for you).
 */
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import EventSource from 'eventsource';

import { ApiClient, SseClient } from '../dist/index.js';

const baseUrl = process.env.AM_BASE_URL ?? 'http://localhost:8899';
const token = process.env.AM_TOKEN;
if (!token) {
  console.error('AM_TOKEN env var is required');
  process.exit(2);
}

const TIMEOUT_MS = Number(process.env.AM_TIMEOUT_MS ?? 90_000);

function log(...args) {
  console.log('[smoke]', ...args);
}

async function main() {
  const api = new ApiClient({ baseUrl, token });
  const sse = new SseClient({ baseUrl, token, EventSource });

  const info = await api.info();
  log('info:', JSON.stringify(info));

  const workspaceDir = mkdtempSync(join(tmpdir(), 'am-smoke-'));
  log('workspaceDir:', workspaceDir);

  const session = await api.createSession({
    workspaceDir,
    title: 'smoke test',
  });
  log('created session:', session.id);

  const assistantTexts = [];
  let sawRunStarted = false;

  const done = new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`timed out after ${TIMEOUT_MS}ms waiting for run_finished`));
    }, TIMEOUT_MS);

    const unsubscribe = sse.subscribe(session.id, {
      afterSeq: 0,
      onEvent: (event) => {
        log(`event seq=${event.seq} type=${event.type}`);
        switch (event.type) {
          case 'run_started':
            sawRunStarted = true;
            break;
          case 'assistant_message':
            assistantTexts.push(String(event.payload?.text ?? ''));
            break;
          case 'error':
            log('error event:', JSON.stringify(event.payload));
            break;
          case 'run_finished': {
            clearTimeout(timer);
            unsubscribe();
            resolve(event.payload?.state ?? 'unknown');
            break;
          }
          default:
            break;
        }
      },
      onError: (err) => {
        // Transport errors trigger auto-reconnect in SseClient; just log.
        log('sse transport error (will retry):', err?.message ?? String(err));
      },
      onReconnect: (afterSeq) => log('sse connected/reconnected from seq', afterSeq),
    });
  });

  const send = await api.send(session.id, {
    message: 'Reply with exactly: hi',
    clientIntentId: crypto.randomUUID(),
  });
  log('send accepted, runId:', send.runId);

  const state = await done;
  log('run finished with state:', state);
  log('saw run_started:', sawRunStarted);

  const reply = assistantTexts.join('\n').trim();
  console.log('\n=== ASSISTANT REPLY ===');
  console.log(reply || '(no assistant_message text captured)');
  console.log('=======================\n');

  // Clean up the session record (best effort).
  try {
    await api.deleteSession(session.id);
    log('deleted session', session.id);
  } catch (err) {
    log('deleteSession failed (non-fatal):', err?.message ?? String(err));
  }

  if (state !== 'done') {
    throw new Error(`run did not finish cleanly: state=${state}`);
  }
  if (!reply) {
    throw new Error('no assistant reply text observed');
  }
}

main().then(
  () => {
    log('SMOKE OK');
    process.exit(0);
  },
  (err) => {
    console.error('[smoke] FAILED:', err?.stack ?? err);
    process.exit(1);
  },
);
