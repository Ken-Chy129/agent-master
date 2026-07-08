import { useEffect, useMemo, useRef, useState } from 'react';
import type { RecentSession } from '@agent-master/core';
import { sessionStatus, statusLine, type SessionStatus } from '../lib/status.js';
import { relTime } from '../lib/time.js';
import { EMPTY_RUNTIME, useStore } from '../store.js';
import { Composer } from './Composer.js';
import { Conversation } from './Conversation.js';
import { IconDots, IconPencil, IconPlus, IconSearch, IconTrash, IconX } from './icons.js';
import { NewSessionModal } from './NewSessionModal.js';

/** One machine's workspace: session list column + the open conversation. */
export function MachineView() {
  const machineId = useStore((s) => s.activeMachineId);
  const currentSessionId = useStore((s) => s.currentSessionId);

  if (!machineId) return null;
  return (
    <div className="flex min-w-0 flex-1">
      <SessionColumn machineId={machineId} />
      <main className="flex min-w-0 flex-1 flex-col bg-canvas">
        {currentSessionId ? (
          <>
            <Conversation />
            <Composer />
          </>
        ) : (
          <div className="m-auto text-sm text-ink-muted">选择一个会话，或新建一个。</div>
        )}
      </main>
    </div>
  );
}

function SessionColumn({ machineId }: { machineId: string }) {
  const machine = useStore((s) => s.machines.find((m) => m.id === machineId) ?? null);
  const runtime = useStore((s) => s.runtimes[machineId] ?? EMPTY_RUNTIME);
  const seenSeq = useStore((s) => s.seenSeq);
  const currentSessionId = useStore((s) => s.currentSessionId);
  const openSession = useStore((s) => s.openSession);
  const removeMachine = useStore((s) => s.removeMachine);

  const [query, setQuery] = useState('');
  const [showNew, setShowNew] = useState(false);
  const [headerMenu, setHeaderMenu] = useState(false);

  const sessions = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q
      ? runtime.sessions.filter(
          (s) =>
            s.title.toLowerCase().includes(q) || s.lastPreview.toLowerCase().includes(q),
        )
      : runtime.sessions;
    // Triage order: attention, running, then the rest by recency.
    const rank: Record<SessionStatus, number> = { attention: 0, running: 1, idle: 2 };
    return [...list].sort((a, b) => {
      const ra = rank[sessionStatus(a, seenSeq[a.id])];
      const rb = rank[sessionStatus(b, seenSeq[b.id])];
      if (ra !== rb) return ra - rb;
      return b.updatedAt.localeCompare(a.updatedAt);
    });
  }, [runtime.sessions, query, seenSeq]);

  if (!machine) return null;
  const claudeAvailable = runtime.info?.providers?.claude?.available;

  return (
    <aside className="flex w-60 flex-none flex-col border-r border-border bg-surface">
      <div className="border-b border-border p-3">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-semibold">{machine.name}</span>
          <span
            className={`h-2 w-2 flex-none rounded-full ${
              runtime.online === null
                ? 'bg-ink-faint'
                : runtime.online
                  ? 'bg-success'
                  : 'bg-danger'
            }`}
            title={runtime.online === false ? '离线' : runtime.online ? '在线' : '检测中'}
          />
          {claudeAvailable === false && (
            <span className="text-[10px] text-danger" title="该机器上找不到 claude CLI">
              claude ✗
            </span>
          )}
          <div className="relative ml-auto">
            <button
              className="rounded-md p-1 text-ink-faint hover:bg-raised hover:text-ink"
              onClick={() => setHeaderMenu((v) => !v)}
              title="机器操作"
            >
              <IconDots size={15} />
            </button>
            {headerMenu && (
              <Menu onClose={() => setHeaderMenu(false)}>
                <MenuItem
                  danger
                  icon={<IconTrash size={13} />}
                  label="移除机器"
                  onClick={() => {
                    setHeaderMenu(false);
                    if (window.confirm(`移除机器「${machine.name}」？（不影响机器上的数据）`)) {
                      void removeMachine(machine.id);
                    }
                  }}
                />
              </Menu>
            )}
          </div>
        </div>
        <div className="mt-1 truncate font-mono text-[10.5px] text-ink-faint" title={machine.baseUrl}>
          {machine.baseUrl}
        </div>
        <div className="relative mt-2">
          <IconSearch size={13} className="absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-faint" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索会话"
            className="w-full rounded-lg border border-border bg-canvas py-1.5 pr-7 pl-8 text-xs outline-none placeholder:text-ink-faint focus:border-accent"
          />
          {query && (
            <button
              onClick={() => setQuery('')}
              className="absolute top-1/2 right-2 -translate-y-1/2 text-ink-faint hover:text-ink"
            >
              <IconX size={12} />
            </button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {runtime.sessionsLoading && runtime.sessions.length === 0 && (
          <div className="p-4 text-center text-xs text-ink-muted">加载中…</div>
        )}
        {!runtime.sessionsLoading && sessions.length === 0 && (
          <div className="p-4 text-center text-xs text-ink-muted">
            {query ? '没有匹配的会话。' : '还没有会话。'}
          </div>
        )}
        {sessions.map((s) => (
          <SessionRow
            key={s.id}
            machineId={machineId}
            session={s}
            status={sessionStatus(s, seenSeq[s.id])}
            active={s.id === currentSessionId}
            onOpen={() => void openSession(machineId, s.id)}
          />
        ))}
      </div>

      <div className="border-t border-border p-3">
        <button
          onClick={() => setShowNew(true)}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg bg-accent py-2 text-xs font-medium text-on-accent transition-opacity hover:opacity-90"
        >
          <IconPlus size={13} />
          新会话
        </button>
      </div>

      {showNew && <NewSessionModal machineId={machineId} onClose={() => setShowNew(false)} />}
    </aside>
  );
}

function SessionRow({
  machineId,
  session,
  status,
  active,
  onOpen,
}: {
  machineId: string;
  session: RecentSession;
  status: SessionStatus;
  active: boolean;
  onOpen: () => void;
}) {
  const renameSession = useStore((s) => s.renameSession);
  const deleteSession = useStore((s) => s.deleteSession);

  const [menu, setMenu] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (renaming) inputRef.current?.select();
  }, [renaming]);

  const commitRename = () => {
    const title = draft.trim();
    setRenaming(false);
    if (title && title !== session.title) void renameSession(machineId, session.id, title);
  };

  const dot =
    status === 'running' ? (
      <span className="pulse-dot mt-1.5 h-2 w-2 flex-none rounded-full bg-accent" />
    ) : status === 'attention' ? (
      <span className="mt-1.5 h-2 w-2 flex-none rounded-full bg-warn-solid" />
    ) : (
      <span className="mt-1.5 h-2 w-2 flex-none rounded-full bg-border-strong" />
    );

  return (
    <div
      className={`group relative flex cursor-pointer gap-2 border-b border-border px-3 py-2.5 transition-colors ${
        active ? 'border-l-2 border-l-accent bg-raised pl-2.5' : 'hover:bg-raised'
      }`}
      onClick={onOpen}
    >
      {dot}
      <div className="min-w-0 flex-1">
        {renaming ? (
          <input
            ref={inputRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename();
              if (e.key === 'Escape') setRenaming(false);
            }}
            onClick={(e) => e.stopPropagation()}
            className="w-full rounded border border-accent bg-surface px-1.5 py-0.5 text-xs outline-none"
          />
        ) : (
          <div className="flex items-baseline gap-2">
            <span className="truncate text-[12.5px] font-medium">
              {session.title || '（未命名）'}
            </span>
            <span className="ml-auto flex-none text-[10px] text-ink-faint">
              {relTime(session.updatedAt)}
            </span>
          </div>
        )}
        <div className="mt-0.5 truncate text-[11px] text-ink-muted">
          {statusLine(session, status)}
        </div>
      </div>

      <div className="relative flex-none self-center">
        <button
          className={`rounded-md p-1 text-ink-faint hover:bg-surface hover:text-ink ${
            menu ? '' : 'opacity-0 group-hover:opacity-100'
          }`}
          onClick={(e) => {
            e.stopPropagation();
            setMenu((v) => !v);
          }}
          title="会话操作"
        >
          <IconDots size={14} />
        </button>
        {menu && (
          <Menu onClose={() => setMenu(false)}>
            <MenuItem
              icon={<IconPencil size={13} />}
              label="重命名"
              onClick={() => {
                setMenu(false);
                setDraft(session.title);
                setRenaming(true);
              }}
            />
            <MenuItem
              danger
              icon={<IconTrash size={13} />}
              label="删除"
              onClick={() => {
                setMenu(false);
                if (window.confirm(`删除会话「${session.title || '（未命名）'}」？此操作不可恢复。`)) {
                  void deleteSession(machineId, session.id);
                }
              }}
            />
          </Menu>
        )}
      </div>
    </div>
  );
}

/** Tiny anchored dropdown; closes on any outside click. */
function Menu({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  useEffect(() => {
    const close = () => onClose();
    window.addEventListener('click', close);
    return () => window.removeEventListener('click', close);
  }, [onClose]);
  return (
    <div
      className="absolute right-0 z-30 mt-1 w-32 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-lg"
      onClick={(e) => e.stopPropagation()}
    >
      {children}
    </div>
  );
}

function MenuItem({
  icon,
  label,
  danger,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  danger?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-raised ${
        danger ? 'text-danger' : 'text-ink'
      }`}
      onClick={onClick}
    >
      {icon}
      {label}
    </button>
  );
}
