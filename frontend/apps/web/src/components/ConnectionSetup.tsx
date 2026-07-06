import { useState } from 'react';
import { useStore } from '../store.js';

const DEFAULT_BASE_URL = 'http://localhost:8888';

export function ConnectionSetup({
  allowCancel = false,
  onDone,
  onCancel,
}: {
  allowCancel?: boolean;
  onDone?: () => void;
  onCancel?: () => void;
}) {
  const addMachine = useStore((s) => s.addMachine);
  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState(DEFAULT_BASE_URL);
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);

  const canSubmit = baseUrl.trim().length > 0 && token.trim().length > 0 && !busy;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    try {
      await addMachine({
        name: name.trim() || undefined,
        baseUrl: baseUrl.trim().replace(/\/+$/, ''),
        token: token.trim(),
      });
      onDone?.();
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="setup" onSubmit={submit}>
      <h1>Add a machine</h1>
      <p>
        Enter an agent-master daemon URL and its token. Run{' '}
        <code>agent-master pair</code> (or <code>agent-master token</code>) on that machine to get
        them.
      </p>

      <div className="field">
        <label htmlFor="name">Name (optional)</label>
        <input
          id="name"
          type="text"
          value={name}
          placeholder="e.g. dev-box"
          onChange={(e) => setName(e.target.value)}
          autoComplete="off"
          spellCheck={false}
        />
      </div>

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

      <div className="setup-actions">
        <button type="submit" className="primary" disabled={!canSubmit}>
          {busy ? 'Adding…' : 'Add machine'}
        </button>
        {allowCancel && (
          <button type="button" onClick={onCancel}>
            Cancel
          </button>
        )}
      </div>
    </form>
  );
}
