import { useState } from 'react';
import { useStore } from '../store.js';
import { NewSessionModal } from './NewSessionModal.js';

export function SessionList() {
  const sessions = useStore((s) => s.sessions);
  const sessionsLoading = useStore((s) => s.sessionsLoading);
  const currentSessionId = useStore((s) => s.currentSessionId);
  const openSession = useStore((s) => s.openSession);
  const refreshSessions = useStore((s) => s.refreshSessions);

  const [showNew, setShowNew] = useState(false);

  return (
    <>
      <div className="sidebar-actions">
        <button className="primary" onClick={() => setShowNew(true)}>
          + New session
        </button>
      </div>
      {showNew && <NewSessionModal onClose={() => setShowNew(false)} />}

      <div className="session-list">
        {sessionsLoading && sessions.length === 0 && (
          <div className="empty" style={{ margin: 0, padding: '16px' }}>
            Loading…
          </div>
        )}
        {!sessionsLoading && sessions.length === 0 && (
          <div className="empty" style={{ margin: 0, padding: '16px' }}>
            No sessions yet.
            <br />
            <button style={{ marginTop: 10 }} onClick={() => void refreshSessions()}>
              Refresh
            </button>
          </div>
        )}
        {sessions.map((s) => (
          <div
            key={s.id}
            className={`session-item${s.id === currentSessionId ? ' active' : ''}`}
            onClick={() => void openSession(s.id)}
          >
            <div className="session-title">{s.title || '(untitled)'}</div>
            <div className="session-meta">
              {s.activeRunId && (
                <span className="run-pill running">
                  <span className="dot pulse" />
                  running
                </span>
              )}
              <span className="session-preview">{s.lastPreview || '—'}</span>
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
