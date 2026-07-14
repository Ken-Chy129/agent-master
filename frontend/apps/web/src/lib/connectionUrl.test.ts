import { describe, expect, it } from 'vitest';
import { defaultDaemonUrl } from './connectionUrl.js';

describe('defaultDaemonUrl', () => {
  it('uses the current HTTP origin for a daemon-hosted Web client', () => {
    expect(defaultDaemonUrl({ protocol: 'http:', origin: 'http://100.64.0.8:8888' })).toBe(
      'http://100.64.0.8:8888',
    );
  });

  it('uses the current HTTPS origin when the daemon is behind a TLS proxy', () => {
    expect(defaultDaemonUrl({ protocol: 'https:', origin: 'https://agent.example.com' })).toBe(
      'https://agent.example.com',
    );
  });

  it('keeps the desktop shell pointed at the local daemon', () => {
    expect(defaultDaemonUrl({ protocol: 'app:', origin: 'null' })).toBe('http://localhost:8888');
  });
});
