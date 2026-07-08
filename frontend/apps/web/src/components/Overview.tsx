import { useMemo, useState } from 'react';
import type { MachineProfile, RecentSession } from '@agent-master/core';
import { projectName } from '../lib/group.js';
import { sessionStatus, statusLine } from '../lib/status.js';
import { relTime, within24h } from '../lib/time.js';
import { EMPTY_RUNTIME, useStore } from '../store.js';
import { IconAlert, IconCheck, IconRefresh, IconPlus } from './icons.js';
import { NewSessionModal } from './NewSessionModal.js';

interface TriagedSession {
  machine: MachineProfile;
  session: RecentSession;
}

/**
 * The cross-machine mission-control page: every session grouped by what it
 * needs from you — attention first, then running, then recently finished.
 */
export function Overview() {
  const machines = useStore((s) => s.machines);
  const runtimes = useStore((s) => s.runtimes);
  const seenSeq = useStore((s) => s.seenSeq);
  const refreshAll = useStore((s) => s.refreshAll);
  const openSession = useStore((s) => s.openSession);

  const [showNew, setShowNew] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const { attention, running, doneRecent, onlineCount, offlineNames } = useMemo(() => {
    const attention: TriagedSession[] = [];
    const running: TriagedSession[] = [];
    const doneRecent: TriagedSession[] = [];
    let onlineCount = 0;
    const offlineNames: string[] = [];

    for (const m of machines) {
      const rt = runtimes[m.id] ?? EMPTY_RUNTIME;
      if (rt.online) onlineCount += 1;
      else if (rt.online === false) offlineNames.push(m.name);
      for (const s of rt.sessions) {
        const status = sessionStatus(s, seenSeq[s.id]);
        if (status === 'running') running.push({ machine: m, session: s });
        else if (status === 'attention') attention.push({ machine: m, session: s });
        else if (s.lastSeq > 0 && within24h(s.updatedAt)) {
          doneRecent.push({ machine: m, session: s });
        }
      }
    }
    const byUpdated = (a: TriagedSession, b: TriagedSession) =>
      b.session.updatedAt.localeCompare(a.session.updatedAt);
    attention.sort(byUpdated);
    running.sort(byUpdated);
    doneRecent.sort(byUpdated);
    return { attention, running, doneRecent, onlineCount, offlineNames };
  }, [machines, runtimes, seenSeq]);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await refreshAll();
    } finally {
      setRefreshing(false);
    }
  };

  const open = (t: TriagedSession) => void openSession(t.machine.id, t.session.id);
  const totalActive = attention.length + running.length;

  return (
    <div className="min-w-0 flex-1 overflow-y-auto">
      <div className="mx-auto max-w-3xl px-6 py-6">
        <header className="mb-5 flex items-center gap-3">
          <div className="min-w-0">
            <h1 className="text-lg font-semibold">任务总览</h1>
            <p className="mt-0.5 text-xs text-ink-muted">
              {machines.length} 台机器 · {onlineCount} 台在线 · {totalActive} 个待关注任务
              {offlineNames.length > 0 && (
                <span className="text-ink-faint">（离线：{offlineNames.join('、')}）</span>
              )}
            </p>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <button
              onClick={() => void refresh()}
              disabled={refreshing}
              className="flex items-center gap-1.5 rounded-lg border border-border bg-surface px-3 py-1.5 text-xs text-ink-muted transition-colors hover:border-border-strong hover:text-ink disabled:opacity-50"
            >
              <IconRefresh size={13} className={refreshing ? 'animate-spin' : ''} />
              刷新
            </button>
            <button
              onClick={() => setShowNew(true)}
              disabled={machines.length === 0}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-on-accent transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              <IconPlus size={13} />
              新会话
            </button>
          </div>
        </header>

        {machines.length === 0 && (
          <EmptyHint text="还没有添加机器。点左侧「+」添加一台跑着 agent-master 的机器。" />
        )}
        {machines.length > 0 &&
          attention.length === 0 &&
          running.length === 0 &&
          doneRecent.length === 0 && (
            <EmptyHint text="一切安静。新建一个会话，把任务交给某台机器上的 agent。" />
          )}

        {attention.length > 0 && (
          <Section
            tone="warn"
            icon={<IconAlert size={15} />}
            title="需要处理"
            count={attention.length}
          >
            {attention.map((t) => (
              <SessionRow key={t.session.id} t={t} kind="attention" onOpen={() => open(t)} />
            ))}
          </Section>
        )}

        {running.length > 0 && (
          <Section
            tone="accent"
            icon={<span className="pulse-dot h-2 w-2 rounded-full bg-accent" />}
            title="进行中"
            count={running.length}
          >
            {running.map((t) => (
              <SessionRow key={t.session.id} t={t} kind="running" onOpen={() => open(t)} />
            ))}
          </Section>
        )}

        {doneRecent.length > 0 && (
          <Section
            tone="muted"
            icon={<IconCheck size={15} className="text-success" />}
            title="最近完成"
            count={doneRecent.length}
          >
            {doneRecent.map((t) => (
              <SessionRow key={t.session.id} t={t} kind="done" onOpen={() => open(t)} />
            ))}
          </Section>
        )}
      </div>

      {showNew && <NewSessionModal onClose={() => setShowNew(false)} />}
    </div>
  );
}

function Section({
  tone,
  icon,
  title,
  count,
  children,
}: {
  tone: 'warn' | 'accent' | 'muted';
  icon: React.ReactNode;
  title: string;
  count: number;
  children: React.ReactNode;
}) {
  const headerCls =
    tone === 'warn'
      ? 'bg-warn-soft text-warn'
      : tone === 'accent'
        ? 'text-accent'
        : 'text-ink-muted';
  return (
    <section
      className={`mb-4 overflow-hidden rounded-xl border bg-surface ${
        tone === 'warn' ? 'border-warn/40' : 'border-border'
      }`}
    >
      <div className={`flex items-center gap-2 px-4 py-2.5 text-xs font-semibold ${headerCls}`}>
        {icon}
        {title}
        <span className="ml-auto font-normal opacity-80">{count}</span>
      </div>
      {children}
    </section>
  );
}

function SessionRow({
  t,
  kind,
  onOpen,
}: {
  t: TriagedSession;
  kind: 'attention' | 'running' | 'done';
  onOpen: () => void;
}) {
  const { machine, session } = t;
  const failed = session.lastRunState === 'failed';
  const line = statusLine(
    session,
    kind === 'running' ? 'running' : kind === 'attention' ? 'attention' : 'idle',
  );
  return (
    <button
      onClick={onOpen}
      className={`flex w-full items-center gap-3 border-t border-border px-4 py-3 text-left transition-colors hover:bg-raised ${
        kind === 'done' ? 'opacity-70' : ''
      }`}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className={`truncate text-[13px] ${kind === 'done' ? '' : 'font-medium'}`}>
            {session.title || '（未命名）'}
          </span>
          <span className="flex-none rounded-full border border-border bg-raised px-2 py-px font-mono text-[10.5px] text-ink-muted">
            {machine.name}
          </span>
          {session.workspaceDir && (
            <span
              className="flex-none font-mono text-[10.5px] text-ink-faint"
              title={session.workspaceDir}
            >
              {projectName(session.workspaceDir)}
            </span>
          )}
        </div>
        {line && (
          <div
            className={`mt-0.5 truncate text-xs ${
              kind === 'attention' && failed ? 'text-danger' : 'text-ink-muted'
            }`}
          >
            {line}
          </div>
        )}
      </div>
      <span className="flex-none text-[11px] text-ink-faint">{relTime(session.updatedAt)}</span>
    </button>
  );
}

function EmptyHint({ text }: { text: string }) {
  return (
    <div className="rounded-xl border border-dashed border-border-strong px-6 py-10 text-center text-sm text-ink-muted">
      {text}
    </div>
  );
}
