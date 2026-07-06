import { useEffect } from 'react';
import { useStore } from './store.js';
import { ConnectionSetup } from './components/ConnectionSetup.js';
import { SessionList } from './components/SessionList.js';
import { Conversation } from './components/Conversation.js';
import { Composer } from './components/Composer.js';

export function App() {
  const connection = useStore((s) => s.connection);
  const currentSessionId = useStore((s) => s.currentSessionId);
  const error = useStore((s) => s.error);
  const clearError = useStore((s) => s.clearError);
  const disconnect = useStore((s) => s.disconnect);
  const refreshSessions = useStore((s) => s.refreshSessions);

  // Load sessions once we have a connection (e.g. restored from localStorage).
  useEffect(() => {
    if (connection) void refreshSessions();
  }, [connection, refreshSessions]);

  if (!connection) {
    return <ConnectionSetup />;
  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="sidebar-header">
          <span className="brand">agent-master</span>
          <span className="host" title={connection.baseUrl}>
            {hostLabel(connection.baseUrl)}
          </span>
        </div>
        <SessionList />
        <div className="sidebar-actions" style={{ borderTop: '1px solid var(--border)', borderBottom: 'none' }}>
          <button onClick={disconnect}>Disconnect</button>
        </div>
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

function hostLabel(baseUrl: string): string {
  try {
    const u = new URL(baseUrl);
    return u.host;
  } catch {
    return baseUrl;
  }
}
