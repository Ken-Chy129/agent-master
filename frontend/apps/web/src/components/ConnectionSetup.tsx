import { useState } from 'react';
import { useStore } from '../store.js';

const DEFAULT_BASE_URL = 'http://localhost:8888';

export function ConnectionSetup() {
  const connect = useStore((s) => s.connect);
  const [baseUrl, setBaseUrl] = useState(DEFAULT_BASE_URL);
  const [token, setToken] = useState('');

  const canSubmit = baseUrl.trim().length > 0 && token.trim().length > 0;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    connect({ baseUrl: baseUrl.trim().replace(/\/+$/, ''), token: token.trim() });
  };

  return (
    <form className="setup" onSubmit={submit}>
      <h1>Connect to a daemon</h1>
      <p>
        Enter the agent-master daemon URL and its token. Run{' '}
        <code>agent-master token</code> on the target machine to get the token.
      </p>

      <div className="field">
        <label htmlFor="baseUrl">Daemon URL</label>
        <input
          id="baseUrl"
          type="text"
          value={baseUrl}
          placeholder={DEFAULT_BASE_URL}
          onChange={(e) => setBaseUrl(e.target.value)}
          autoComplete="off"
          spellCheck={false}
        />
      </div>

      <div className="field">
        <label htmlFor="token">Token</label>
        <input
          id="token"
          type="password"
          value={token}
          placeholder="Bearer token"
          onChange={(e) => setToken(e.target.value)}
          autoComplete="off"
          spellCheck={false}
        />
      </div>

      <button type="submit" className="primary" disabled={!canSubmit} style={{ width: '100%' }}>
        Connect
      </button>
    </form>
  );
}
