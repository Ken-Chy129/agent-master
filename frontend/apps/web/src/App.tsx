import { useEffect, useState } from 'react';
import { useStore } from './store.js';
import { ConnectionSetup } from './components/ConnectionSetup.js';
import { MachineSwitcher } from './components/MachineSwitcher.js';
import { SessionList } from './components/SessionList.js';
import { Conversation } from './components/Conversation.js';
import { Composer } from './components/Composer.js';

export function App() {
  const initialized = useStore((s) => s.initialized);
  const machines = useStore((s) => s.machines);
  const currentSessionId = useStore((s) => s.currentSessionId);
  const error = useStore((s) => s.error);
  const clearError = useStore((s) => s.clearError);
  const init = useStore((s) => s.init);

  const [adding, setAdding] = useState(false);

  // Load persisted machines on mount (localStorage on web, secure store on desktop).
  useEffect(() => {
    void init();
  }, [init]);

  if (!initialized) {
    return <div className="empty">Loading…</div>;
  }

  const showSetup = machines.length === 0 || adding;
  if (showSetup) {
    return (
      <ConnectionSetup
        allowCancel={machines.length > 0}
        onDone={() => setAdding(false)}
        onCancel={() => setAdding(false)}
      />
    );
  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="sidebar-header">
          <span className="brand">agent-master</span>
          <MachineSwitcher onAdd={() => setAdding(true)} />
        </div>
        <SessionList />
      </aside>

      <main className="main">
        {error && (
          <div className="banner">
            <span>{error}</span>
            <button onClick={clearError}>Dismiss</button>
          </div>
        )}
        {currentSessionId ? (
          <>
            <Conversation />
            <Composer />
          </>
        ) : (
          <div className="empty">Select a session, or create a new one.</div>
        )}
      </main>
    </div>
  );
}
