import { useEffect, useState } from 'react';
import type { WorkspaceListing } from '@agent-master/core';
import { useStore } from '../store.js';

/** Modal to pick a workspace directory (browsing the daemon's filesystem) and create a session. */
export function NewSessionModal({ onClose }: { onClose: () => void }) {
  const listWorkspaces = useStore((s) => s.listWorkspaces);
  const createSession = useStore((s) => s.createSession);

  const [listing, setListing] = useState<WorkspaceListing | null>(null);
  const [loading, setLoading] = useState(true);
  const [title, setTitle] = useState('');
  const [creating, setCreating] = useState(false);

  const browse = async (path?: string) => {
    setLoading(true);
    const res = await listWorkspaces(path);
    if (res) setListing(res);
    setLoading(false);
  };

  useEffect(() => {
    void browse(undefined);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const current = listing?.path ?? '';
  const canCreate = current.length > 0 && !creating;

  const create = async () => {
    if (!canCreate) return;
    setCreating(true);
    try {
      await createSession({ workspaceDir: current, title: title.trim() || undefined });
      onClose();
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>New session</h2>
        <p className="modal-sub">Pick a working directory on the daemon machine.</p>

        <div className="picker-path">{current || 'Roots'}</div>

        <div className="picker-list">
          {loading && <div className="empty" style={{ padding: 16 }}>Loading…</div>}
          {!loading && listing?.parent && (
            <button className="picker-item up" onClick={() => void browse(listing.parent)}>
              ↑ ..
            </button>
          )}
          {!loading &&
            listing?.entries.map((e) => (
              <button key={e.path} className="picker-item" onClick={() => void browse(e.path)}>
                📁 {e.name}
              </button>
            ))}
          {!loading && listing && listing.entries.length === 0 && (
            <div className="empty" style={{ padding: 16 }}>No sub-directories.</div>
          )}
        </div>

        <div className="field">
          <label htmlFor="sess-title">Title (optional)</label>
          <input
            id="sess-title"
            type="text"
            value={title}
            placeholder="e.g. fix login bug"
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>

        <div className="modal-actions">
          <button onClick={onClose}>Cancel</button>
          <button className="primary" disabled={!canCreate} onClick={create}>
            {creating ? 'Creating…' : current ? `Create here` : 'Choose a directory'}
          </button>
        </div>
      </div>
    </div>
  );
}
