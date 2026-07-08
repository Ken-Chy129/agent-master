import { useEffect, useState } from 'react';
import type { WorkspaceListing } from '@agent-master/core';
import { EMPTY_RUNTIME, useStore } from '../store.js';
import { IconArrowUp, IconFolder } from './icons.js';

const MODEL_OPTIONS = [
  { value: '', label: '默认模型' },
  { value: 'sonnet', label: 'Sonnet' },
  { value: 'opus', label: 'Opus' },
  { value: 'haiku', label: 'Haiku' },
];

/**
 * Modal to create a session: pick a machine (when not fixed), browse its
 * filesystem for a workspace directory (starting from `initialDir` when
 * given, e.g. quick-create from a project group), optionally set title/model.
 */
export function NewSessionModal({
  machineId,
  initialDir,
  onClose,
}: {
  machineId?: string;
  initialDir?: string;
  onClose: () => void;
}) {
  const machines = useStore((s) => s.machines);
  const runtimes = useStore((s) => s.runtimes);
  const listWorkspaces = useStore((s) => s.listWorkspaces);
  const createSession = useStore((s) => s.createSession);

  const [selectedMachine, setSelectedMachine] = useState<string>(() => {
    if (machineId) return machineId;
    const online = machines.find((m) => (runtimes[m.id] ?? EMPTY_RUNTIME).online);
    return (online ?? machines[0])?.id ?? '';
  });
  const [listing, setListing] = useState<WorkspaceListing | null>(null);
  const [loading, setLoading] = useState(true);
  const [title, setTitle] = useState('');
  const [model, setModel] = useState('');
  const [creating, setCreating] = useState(false);

  const browse = async (path?: string) => {
    if (!selectedMachine) return;
    setLoading(true);
    const res = await listWorkspaces(selectedMachine, path);
    if (res) setListing(res);
    setLoading(false);
  };

  useEffect(() => {
    setListing(null);
    void browse(initialDir);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedMachine]);

  const current = listing?.path ?? '';
  const canCreate = selectedMachine.length > 0 && current.length > 0 && !creating;

  const create = async () => {
    if (!canCreate) return;
    setCreating(true);
    try {
      await createSession(selectedMachine, {
        workspaceDir: current,
        title: title.trim() || undefined,
        model: model || undefined,
      });
      onClose();
    } finally {
      setCreating(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="flex max-h-[85vh] w-full max-w-lg flex-col gap-3 rounded-2xl border border-border bg-surface p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div>
          <h2 className="text-base font-semibold">新会话</h2>
          <p className="mt-0.5 text-xs text-ink-muted">选择目标机器和它上面的工作目录。</p>
        </div>

        {!machineId && (
          <label className="block">
            <span className="mb-1 block text-xs text-ink-muted">机器</span>
            <select
              value={selectedMachine}
              onChange={(e) => setSelectedMachine(e.target.value)}
              className="w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-accent"
            >
              {machines.map((m) => {
                const rt = runtimes[m.id] ?? EMPTY_RUNTIME;
                return (
                  <option key={m.id} value={m.id}>
                    {m.name}
                    {rt.online === false ? '（离线）' : ''}
                  </option>
                );
              })}
            </select>
          </label>
        )}

        <div className="truncate rounded-lg border border-border bg-raised px-3 py-2 font-mono text-xs text-ink-muted">
          {current || '（根目录）'}
        </div>

        <div className="min-h-40 flex-1 overflow-y-auto rounded-lg border border-border">
          {loading && <div className="p-4 text-center text-sm text-ink-muted">加载中…</div>}
          {!loading && listing?.parent && (
            <button
              className="flex w-full items-center gap-2 border-b border-border px-3 py-2 text-left text-sm text-ink-muted hover:bg-raised"
              onClick={() => void browse(listing.parent)}
            >
              <IconArrowUp size={14} />
              上一级
            </button>
          )}
          {!loading &&
            listing?.entries.map((e) => (
              <button
                key={e.path}
                className="flex w-full items-center gap-2 border-b border-border px-3 py-2 text-left text-sm hover:bg-raised"
                onClick={() => void browse(e.path)}
              >
                <IconFolder size={14} className="text-ink-faint" />
                {e.name}
              </button>
            ))}
          {!loading && listing && listing.entries.length === 0 && (
            <div className="p-4 text-center text-sm text-ink-muted">没有子目录。</div>
          )}
        </div>

        <div className="flex gap-3">
          <label className="block flex-1">
            <span className="mb-1 block text-xs text-ink-muted">标题（可选）</span>
            <input
              type="text"
              value={title}
              placeholder="例如：修复登录 bug"
              onChange={(e) => setTitle(e.target.value)}
              className="w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none placeholder:text-ink-faint focus:border-accent"
            />
          </label>
          <label className="block w-32">
            <span className="mb-1 block text-xs text-ink-muted">模型</span>
            <select
              value={model}
              onChange={(e) => setModel(e.target.value)}
              className="w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-accent"
            >
              {MODEL_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="flex justify-end gap-2 pt-1">
          <button
            onClick={onClose}
            className="rounded-lg border border-border px-4 py-2 text-sm text-ink-muted transition-colors hover:border-border-strong hover:text-ink"
          >
            取消
          </button>
          <button
            disabled={!canCreate}
            onClick={() => void create()}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-on-accent transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {creating ? '创建中…' : '在此目录创建'}
          </button>
        </div>
      </div>
    </div>
  );
}
