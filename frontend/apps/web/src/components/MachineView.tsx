import { useEffect, useMemo, useRef, useState } from 'react';
import type { RecentSession } from '@agent-master/core';
import { GROUP_MODES, groupSessions, type GroupMode } from '../lib/group.js';
import { sessionStatus, statusLine, type SessionStatus } from '../lib/status.js';
import { relTime } from '../lib/time.js';
import { EMPTY_RUNTIME, useStore } from '../store.js';
import { Conversation } from './Conversation.js';
import {
  IconAlert,
  IconChevronRight,
  IconDots,
  IconPanelLeft,
  IconPencil,
  IconPlus,
  IconSearch,
  IconTrash,
  IconX,
} from './icons.js';
import { ConfirmDialog } from './ConfirmDialog.js';
import { Menu, MenuItem } from './Menu.js';
import { NewSessionModal } from './NewSessionModal.js';
import { SelectMenu } from './SelectMenu.js';

const GROUP_MODE_KEY = 'agent-master.groupMode';
const COLLAPSED_KEY = 'agent-master.collapsedGroups';

function loadGroupMode(): GroupMode {
  try {
    const v = localStorage.getItem(GROUP_MODE_KEY);
    if (v === 'project' || v === 'updated' || v === 'created') return v;
  } catch {
    /* ignore */
  }
  return 'project';
}

function loadCollapsed(): Set<string> {
  try {
    const raw = localStorage.getItem(COLLAPSED_KEY);
    if (raw) return new Set(JSON.parse(raw) as string[]);
  } catch {
    /* ignore */
  }
  return new Set();
}

/** One machine's workspace: session list column (collapsible) + the open conversation. */
export function MachineView() {
  const machineId = useStore((s) => s.activeMachineId);
  const currentSessionId = useStore((s) => s.currentSessionId);
  const columnCollapsed = useStore((s) => s.sessionColumnCollapsed);
  const toggleColumn = useStore((s) => s.toggleSessionColumn);

  if (!machineId) return null;
  return (
    <div className="flex min-w-0 flex-1">
      {!columnCollapsed && <SessionColumn machineId={machineId} onCollapse={toggleColumn} />}
      <main className="relative flex min-w-0 flex-1 flex-col bg-canvas">
        {currentSessionId ? (
          <Conversation />
        ) : (
          <>
            {columnCollapsed && (
              <button
                title="展开会话列表"
                onClick={toggleColumn}
                className="absolute top-3 left-3 rounded-md p-1.5 text-ink-faint hover:bg-raised hover:text-ink"
              >
                <IconPanelLeft size={15} />
              </button>
            )}
            <div className="m-auto text-center text-sm text-ink-muted">
              <p>选择一个会话，或新建一个。</p>
            </div>
          </>
        )}
      </main>
    </div>
  );
}

function SessionColumn({
  machineId,
  onCollapse,
}: {
  machineId: string;
  onCollapse: () => void;
}) {
  const machine = useStore((s) => s.machines.find((m) => m.id === machineId) ?? null);
  const runtime = useStore((s) => s.runtimes[machineId] ?? EMPTY_RUNTIME);
  const seenSeq = useStore((s) => s.seenSeq);
  const currentSessionId = useStore((s) => s.currentSessionId);
  const openSession = useStore((s) => s.openSession);

  const [query, setQuery] = useState('');
  const [groupMode, setGroupMode] = useState<GroupMode>(loadGroupMode);
  const [collapsed, setCollapsed] = useState<Set<string>>(loadCollapsed);
  const [newSession, setNewSession] = useState<{ initialDir?: string } | null>(null);

  const changeGroupMode = (m: GroupMode) => {
    setGroupMode(m);
    try {
      localStorage.setItem(GROUP_MODE_KEY, m);
    } catch {
      /* ignore */
    }
  };

  const toggleGroup = (key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      const full = `${machineId}:${key}`;
      if (next.has(full)) next.delete(full);
      else next.add(full);
      try {
        localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]));
      } catch {
        /* ignore */
      }
      return next;
    });
  };

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q
      ? runtime.sessions.filter(
          (s) =>
            s.title.toLowerCase().includes(q) || s.lastPreview.toLowerCase().includes(q),
        )
      : runtime.sessions;
    return groupSessions(list, groupMode);
  }, [runtime.sessions, query, groupMode]);

  if (!machine) return null;
  const claudeAvailable = runtime.info?.providers?.claude?.available;

  return (
    <aside className="session-column flex flex-none flex-col border-r border-border bg-surface">
      <div className="app-drag px-4 pt-4 pb-3">
        <div className="flex items-center gap-2">
          <span className="truncate text-[15px] font-semibold tracking-[-0.015em]">{machine.name}</span>
          <span
            className={`h-1.5 w-1.5 flex-none rounded-full ${
              runtime.online === null
                ? 'bg-ink-faint'
                : runtime.online
                  ? 'bg-success'
                  : 'bg-danger'
            }`}
            title={runtime.online === false ? '离线' : runtime.online ? '在线' : '检测中'}
          />
          {claudeAvailable === false && (
            <span
              className="flex text-danger"
              title="该机器上找不到 Claude CLI"
              aria-label="Claude CLI 不可用"
            >
              <IconAlert size={13} />
            </span>
          )}
          <button
            className="ml-auto rounded-lg p-1.5 text-ink-faint hover:bg-raised hover:text-ink"
            onClick={onCollapse}
            title="收起会话列表"
            aria-label="收起会话列表"
          >
            <IconPanelLeft size={16} />
          </button>
        </div>
        <div
          className="mt-0.5 truncate font-mono text-[10.5px] text-ink-faint"
          title={machine.baseUrl}
        >
          {machine.baseUrl}
        </div>

        <div className="mt-3.5 flex items-center gap-2">
          <div className="relative flex-1">
            <IconSearch
              size={13}
              className="absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-faint"
            />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索会话"
              aria-label="搜索会话"
              className="session-search w-full rounded-xl py-2 pr-7 pl-8 text-[12.5px] outline-none placeholder:text-ink-faint"
            />
            {query && (
              <button
                onClick={() => setQuery('')}
                aria-label="清空搜索"
                className="absolute top-1/2 right-2 -translate-y-1/2 text-ink-faint hover:text-ink"
              >
                <IconX size={12} />
              </button>
            )}
          </div>
          <SelectMenu
            value={groupMode}
            onChange={(value) => changeGroupMode(value as GroupMode)}
            options={GROUP_MODES.map((mode) => ({ value: mode.value, label: mode.label }))}
            ariaLabel="会话归类方式"
            placement="bottom"
            buttonClassName="session-group-trigger w-[104px] py-2 text-[11.5px]"
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        {runtime.sessionsLoading && runtime.sessions.length === 0 && (
          <div className="p-4 text-center text-xs text-ink-muted">加载中…</div>
        )}
        {!runtime.sessionsLoading && groups.length === 0 && (
          <div className="p-4 text-center text-xs text-ink-muted">
            {query ? '没有匹配的会话。' : '还没有会话。'}
          </div>
        )}
        {groups.map((g) => {
          const isCollapsed = collapsed.has(`${machineId}:${g.key}`);
          return (
            <div key={g.key} className="mt-3 first:mt-1">
              <div className="group/header session-group-header flex items-center rounded-lg">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-1.5 px-2.5 py-2 text-left select-none"
                  title={g.dir}
                  onClick={() => toggleGroup(g.key)}
                  aria-expanded={!isCollapsed}
                >
                  <IconChevronRight
                    size={11}
                    className={`flex-none text-ink-faint transition-transform ${
                      isCollapsed ? '' : 'rotate-90'
                    }`}
                  />
                  <span className="truncate text-[11.5px] font-semibold text-ink-muted">{g.label}</span>
                  <span className="flex-none text-[10.5px] text-ink-faint">{g.sessions.length}</span>
                </button>
                {groupMode === 'project' && g.dir && (
                  <button
                    title={`在 ${g.label} 新建会话`}
                    onClick={(e) => {
                      e.stopPropagation();
                      setNewSession({ initialDir: g.dir });
                    }}
                    aria-label={`在 ${g.label} 新建会话`}
                    className="mr-1 rounded-md p-1 text-ink-faint opacity-0 group-hover/header:opacity-100 hover:bg-raised hover:text-ink focus:opacity-100"
                  >
                    <IconPlus size={13} />
                  </button>
                )}
              </div>
              {!isCollapsed &&
                g.sessions.map((s) => (
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
          );
        })}
      </div>

      <div className="px-3 pt-2 pb-3">
        <button
          onClick={() => setNewSession({})}
          className="session-new-button flex w-full items-center justify-center gap-2 rounded-xl py-2.5 text-[12.5px] font-medium transition-all"
        >
          <IconPlus size={14} />
          新建会话
        </button>
      </div>

      {newSession && (
        <NewSessionModal
          machineId={machineId}
          initialDir={newSession.initialDir}
          onClose={() => setNewSession(null)}
        />
      )}
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
  const [confirmDelete, setConfirmDelete] = useState(false);
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

  return (
    <div
      className={`session-row group relative flex cursor-pointer gap-2 rounded-xl px-3 py-2.5 transition-colors ${
        active ? 'session-row-active' : ''
      }`}
    >
      {renaming ? (
        <div className="min-w-0 flex-1">
          <input
            ref={inputRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              // Ignore Enter while an IME composition is in progress.
              if (e.key === 'Enter' && !e.nativeEvent.isComposing && e.keyCode !== 229) {
                commitRename();
              }
              if (e.key === 'Escape') setRenaming(false);
            }}
            onClick={(e) => e.stopPropagation()}
            className="w-full rounded border border-accent bg-surface px-1.5 py-0.5 text-xs outline-none"
          />
        </div>
      ) : (
        <button
          type="button"
          onClick={onOpen}
          aria-current={active ? 'true' : undefined}
          className="min-w-0 flex-1 text-left"
        >
          <div className="flex items-center gap-2">
            {status === 'running' && (
              <span className="pulse-dot h-1.5 w-1.5 flex-none rounded-full bg-accent" />
            )}
            {status === 'attention' && (
              <span className="h-1.5 w-1.5 flex-none rounded-full bg-warn-solid" />
            )}
            <span
              className={`truncate text-[13px] leading-5 ${
                active ? 'font-semibold text-ink' : 'font-medium text-ink'
              }`}
            >
              {session.title || '（未命名）'}
            </span>
            <span className="ml-auto flex-none text-[10.5px] text-ink-faint group-hover:hidden">
              {relTime(session.updatedAt)}
            </span>
          </div>
          {statusLine(session, status) && (
            <div className="mt-0.5 truncate text-[11.5px] leading-5 text-ink-faint">
              {statusLine(session, status)}
            </div>
          )}
        </button>
      )}

      <div className="absolute top-2.5 right-2.5">
        <button
          className={`rounded-md p-0.5 text-ink-faint hover:bg-surface hover:text-ink ${
            menu ? '' : 'hidden group-hover:block'
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
                setConfirmDelete(true);
              }}
            />
          </Menu>
        )}
      </div>

      {confirmDelete && (
        <ConfirmDialog
          title={`删除会话「${session.title || '（未命名）'}」？`}
          description="会话及其全部消息记录将被永久删除，此操作不可恢复。"
          confirmLabel="删除"
          onConfirm={() => {
            setConfirmDelete(false);
            void deleteSession(machineId, session.id);
          }}
          onCancel={() => setConfirmDelete(false)}
        />
      )}
    </div>
  );
}
