import { describe, expect, it } from 'vitest';
import { machineReadiness } from './readiness.js';
import type { InfoResponse } from './types.js';

const base: InfoResponse = {
  name: 'box',
  version: '0.3.0',
  providers: { claude: { available: true, path: '/usr/local/bin/claude' } },
};

describe('machineReadiness', () => {
  it('is ready when the daemon reports a credential and a CLI', () => {
    const r = machineReadiness({ ...base, auth: { ok: true, source: 'ANTHROPIC_API_KEY' } });
    expect(r.ready).toBe(true);
    expect(r.reason).toBeNull();
    expect(r.refusing).toBe(false);
  });

  // The month-long failure: reachable, answering, and unable to run anything.
  it('is not ready when no credential was found', () => {
    const r = machineReadiness({ ...base, auth: { ok: false, hint: 'run claude /login' } });
    expect(r.ready).toBe(false);
    expect(r.refusing).toBe(false);
    expect(r.reason).toContain('凭证');
  });

  // A blocked shell env is the more severe state and the cause of the missing
  // credential it would otherwise be reported as — report it alone.
  it('reports a refused shell environment instead of the credential it implies', () => {
    const r = machineReadiness({
      ...base,
      auth: { ok: false },
      shell_env: { blocked: true, missing: ['ANTHROPIC_API_KEY', 'ANTHROPIC_BASE_URL'] },
    });
    expect(r.ready).toBe(false);
    expect(r.refusing).toBe(true);
    expect(r.reason).toContain('ANTHROPIC_API_KEY');
    expect(r.reason).toContain('拒绝');
  });

  it('is not ready when the claude CLI is missing', () => {
    const r = machineReadiness({
      ...base,
      providers: { claude: { available: false } },
      auth: { ok: true, source: 'ANTHROPIC_API_KEY' },
    });
    expect(r.ready).toBe(false);
    expect(r.reason).toContain('Claude CLI');
  });

  // An older daemon omits these fields. Absent must read as "cannot tell", not
  // "broken", or every un-upgraded machine would be flagged unusable.
  it('treats an older daemon as ready rather than broken', () => {
    expect(machineReadiness(base).ready).toBe(true);
    expect(machineReadiness({ ...base, auth: {} }).ready).toBe(true);
    expect(machineReadiness({ ...base, shell_env: {} }).ready).toBe(true);
  });

  it('treats a machine that has not been probed as ready', () => {
    expect(machineReadiness(null).ready).toBe(true);
    expect(machineReadiness(undefined).ready).toBe(true);
  });
});
