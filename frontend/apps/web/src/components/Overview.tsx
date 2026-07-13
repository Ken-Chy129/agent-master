import { useMemo, useState } from 'react';
import type { MachineProfile, RecentSession } from '@agent-master/core';
import { projectName } from '../lib/group.js';
import { sessionStatus, statusLine } from '../lib/status.js';
import { relTime, within24h } from '../lib/time.js';
import { EMPTY_RUNTIME, useStore } from '../store.js';
import { IconAlert, IconCheck, IconChevronRight, IconRefresh, IconPlus } from './icons.js';
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
  const openMachine = useStore((s) => s.openMachine);

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

  return (
    <main className="overview-canvas min-w-0 flex-1 overflow-y-auto">
      <div className="mx-auto max-w-[1080px] px-7 py-7">
        <header className="app-drag mb-5 flex items-start gap-4">
          <div className="min-w-0">
            <div className="mb-1 font-mono text-[10px] font-medium tracking-[0.18em] text-ink-faint uppercase">
              Agent Master / Mission Control
            </div>
            <h1 className="overview-title text-[26px] leading-tight font-semibold tracking-[-0.035em]">
              任务控制台
            </h1>
            <p className="mt-1 text-[12.5px] text-ink-muted">跨机器查看进度，优先处理真正需要你的任务。</p>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <button
              onClick={() => void refresh()}
              disabled={refreshing}
              className="flex items-center gap-1.5 rounded-lg border border-border bg-surface px-3 py-1.5 text-xs text-ink-muted shadow-[0_1px_2px_rgba(0,0,0,0.03)] transition-all hover:-translate-y-px hover:border-border-strong hover:text-ink disabled:opacity-50"
            >
              <IconRefresh size={13} className={refreshing ? 'animate-spin' : ''} />
              刷新
            </button>
            <button
              onClick={() => setShowNew(true)}
              disabled={machines.length === 0}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-on-accent shadow-[0_5px_14px_color-mix(in_srgb,var(--am-accent)_24%,transparent)] transition-all hover:-translate-y-px hover:brightness-105 disabled:opacity-50"
            >
              <IconPlus size={13} />
              新会话
            </button>
          </div>
        </header>

        <SignalStrip
          machines={machines.length}
          online={onlineCount}
          attention={attention.length}
          running={running.length}
          recent={doneRecent.length}
        />

        <div className="mt-5 grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_260px]">
          <div className="min-w-0">
            {machines.length === 0 && (
              <EmptyHint
                title="先连接一台机器"
                text="点左侧的加号，添加一台正在运行 agent-master 的 Mac 或开发机。"
              />
            )}
            {machines.length > 0 &&
              attention.length === 0 &&
              running.length === 0 &&
              doneRecent.length === 0 && (
                <EmptyHint
                  title="任务台暂时空闲"
                  text="新建一个会话，把下一项工作交给任意一台在线机器。"
                />
              )}

            {attention.length > 0 && (
              <Section
                tone="warn"
                icon={<IconAlert size={14} />}
                title="需要处理"
                subtitle="已有结果，等待你决定下一步"
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
                title="正在运行"
                subtitle="Agent 正在这些工作区执行任务"
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
                icon={<IconCheck size={14} className="text-success" />}
                title="最近完成"
                subtitle="过去 24 小时已结束的任务"
                count={doneRecent.length}
              >
                {doneRecent.map((t) => (
                  <SessionRow key={t.session.id} t={t} kind="done" onOpen={() => open(t)} />
                ))}
              </Section>
            )}
          </div>

          <MachinePanel
            machines={machines}
            runtimes={runtimes}
            offlineNames={offlineNames}
            onOpen={openMachine}
          />
        </div>
      </div>

      {showNew && <NewSessionModal onClose={() => setShowNew(false)} />}
    </main>
  );
}

function SignalStrip({
  machines,
  online,
  attention,
  running,
  recent,
}: {
  machines: number;
  online: number;
  attention: number;
  running: number;
  recent: number;
}) {
  const stats = [
    { label: '在线机器', value: `${online}/${machines}`, tone: 'success' },
    { label: '需要处理', value: attention, tone: attention > 0 ? 'warn' : 'muted' },
    { label: '正在运行', value: running, tone: running > 0 ? 'accent' : 'muted' },
    { label: '24h 完成', value: recent, tone: 'muted' },
  ];
  return (
    <section aria-label="任务状态摘要" className="overview-signal-strip">
      {stats.map((stat) => (
        <div key={stat.label} className="overview-signal-segment">
          <span className={`signal-dot signal-dot-${stat.tone}`} aria-hidden="true" />
          <span className="text-[11px] text-ink-muted">{stat.label}</span>
          <strong className="ml-auto font-mono text-[13px] font-semibold text-ink">{stat.value}</strong>
        </div>
      ))}
    </section>
  );
}

function Section({
  tone,
  icon,
  title,
  subtitle,
  count,
  children,
}: {
  tone: 'warn' | 'accent' | 'muted';
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  count: number;
  children: React.ReactNode;
}) {
  return (
    <section
      className={`task-lane task-lane-${tone} mb-4 overflow-hidden rounded-xl border bg-surface`}
    >
      <div className="flex items-center gap-2.5 px-4 py-3">
        <span className="flex h-6 w-6 flex-none items-center justify-center rounded-md bg-raised text-ink-muted">
          {icon}
        </span>
        <div className="min-w-0">
          <h2 className="text-[12.5px] font-semibold">{title}</h2>
          <p className="truncate text-[10.5px] text-ink-faint">{subtitle}</p>
        </div>
        <span className="ml-auto rounded-md bg-raised px-2 py-0.5 font-mono text-[10.5px] text-ink-muted">
          {count}
        </span>
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
      className={`group flex w-full items-center gap-3 border-t border-border px-4 py-3 text-left transition-colors hover:bg-raised/70 ${
        kind === 'done' ? 'opacity-75 hover:opacity-100' : ''
      }`}
    >
      <span
        className={`h-7 w-1 flex-none rounded-full ${
          kind === 'attention'
            ? 'bg-warn-solid'
            : kind === 'running'
              ? 'pulse-dot bg-accent'
              : 'bg-success/45'
        }`}
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className={`truncate text-[13px] ${kind === 'done' ? '' : 'font-medium'}`}>
            {session.title || '（未命名）'}
          </span>
          <span className="flex-none rounded-md border border-border bg-raised/70 px-1.5 py-px font-mono text-[10px] text-ink-muted">
            {machine.name}
          </span>
          {session.workspaceDir && (
            <span
              className="min-w-0 truncate font-mono text-[10px] text-ink-faint"
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
      <div className="flex flex-none items-center gap-2 text-[11px] text-ink-faint">
        {relTime(session.updatedAt)}
        <IconChevronRight
          size={13}
          className="translate-x-0 opacity-0 transition-all group-hover:translate-x-0.5 group-hover:opacity-100"
        />
      </div>
    </button>
  );
}

function MachinePanel({
  machines,
  runtimes,
  offlineNames,
  onOpen,
}: {
  machines: MachineProfile[];
  runtimes: ReturnType<typeof useStore.getState>['runtimes'];
  offlineNames: string[];
  onOpen: (id: string) => void;
}) {
  if (machines.length === 0) return null;
  return (
    <aside className="machine-signal-panel overflow-hidden rounded-xl border border-border bg-surface">
      <div className="border-b border-border px-4 py-3">
        <h2 className="text-[12.5px] font-semibold">机器信号</h2>
        <p className="mt-0.5 text-[10.5px] text-ink-faint">
          {offlineNames.length > 0 ? `${offlineNames.length} 台离线` : '所有机器连接正常'}
        </p>
      </div>
      <div className="divide-y divide-border">
        {machines.map((machine) => {
          const runtime = runtimes[machine.id] ?? EMPTY_RUNTIME;
          const running = runtime.sessions.filter((session) => session.activeRunId).length;
          return (
            <button
              key={machine.id}
              onClick={() => onOpen(machine.id)}
              className="group flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-raised/70"
            >
              <span
                className={`h-2 w-2 flex-none rounded-full ${
                  runtime.online === null
                    ? 'bg-ink-faint'
                    : runtime.online
                      ? 'bg-success'
                      : 'bg-danger'
                }`}
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[12px] font-medium">{machine.name}</span>
                <span className="block truncate font-mono text-[9.5px] text-ink-faint">
                  {machine.baseUrl.replace(/^https?:\/\//, '')}
                </span>
              </span>
              <span className="text-right">
                <span className="block font-mono text-[11px] font-semibold text-ink">
                  {running > 0 ? `${running} 运行中` : `${runtime.sessions.length} 会话`}
                </span>
                <span className="block text-[9.5px] text-ink-faint">
                  {runtime.online === null ? '检测中' : runtime.online ? '在线' : '离线'}
                </span>
              </span>
            </button>
          );
        })}
      </div>
    </aside>
  );
}

function EmptyHint({ title, text }: { title: string; text: string }) {
  return (
    <div className="rounded-xl border border-dashed border-border-strong bg-surface/60 px-6 py-12 text-center">
      <h2 className="text-sm font-semibold text-ink">{title}</h2>
      <p className="mx-auto mt-1 max-w-md text-xs leading-relaxed text-ink-muted">{text}</p>
    </div>
  );
}
