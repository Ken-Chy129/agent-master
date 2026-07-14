import { useState } from 'react';
import type { MachineProfile } from '@agent-master/core';
import { EMPTY_RUNTIME, useStore } from '../store.js';
import { ConfirmDialog } from './ConfirmDialog.js';
import { IconGrid, IconPlus, IconTrash } from './icons.js';
import { Menu, MenuItem } from './Menu.js';

/** Short uppercase initials for a machine avatar, e.g. "dev-01" -> "DEV". */
export function machineInitials(name: string): string {
  const clean = name.replace(/[^\p{L}\p{N}]/gu, '');
  return (clean || '?').slice(0, 3).toUpperCase();
}

/**
 * The far-left machine rail: overview entry, one avatar per machine (online
 * dot + active-run badge; right-click to manage), and "add machine".
 */
export function Rail({ onAddMachine }: { onAddMachine: () => void }) {
  const machines = useStore((s) => s.machines);
  const runtimes = useStore((s) => s.runtimes);
  const view = useStore((s) => s.view);
  const activeMachineId = useStore((s) => s.activeMachineId);
  const openOverview = useStore((s) => s.openOverview);
  const openMachine = useStore((s) => s.openMachine);

  return (
    <nav
      aria-label="机器导航"
      className="am-rail app-drag flex w-[68px] flex-none flex-col items-center gap-2 py-3"
    >
      <button
        title="任务总览"
        aria-label="任务总览"
        onClick={openOverview}
        className={`rail-control relative flex h-10 w-10 items-center justify-center rounded-[13px] transition-all ${
          view === 'overview'
            ? 'rail-control-active'
            : 'hover:-translate-y-px hover:bg-white/10'
        }`}
      >
        <IconGrid size={19} />
        {view === 'overview' && <span className="rail-active-indicator" aria-hidden="true" />}
      </button>

      <div className="my-1 h-px w-7 bg-white/10" />

      {machines.map((m) => (
        <MachineAvatar
          key={m.id}
          machine={m}
          active={view === 'machine' && activeMachineId === m.id}
          onClick={() => openMachine(m.id)}
          runningCount={
            (runtimes[m.id] ?? EMPTY_RUNTIME).sessions.filter((s) => s.activeRunId).length
          }
          online={(runtimes[m.id] ?? EMPTY_RUNTIME).online}
        />
      ))}

      <button
        title="添加机器"
        aria-label="添加机器"
        onClick={onAddMachine}
        className="rail-control flex h-10 w-10 items-center justify-center rounded-[13px] border border-dashed border-white/20 transition-all hover:-translate-y-px hover:border-white/40 hover:bg-white/10"
      >
        <IconPlus size={17} />
      </button>

      <div className="mt-auto mb-1 h-1.5 w-1.5 rounded-full bg-white/25" aria-hidden="true" />
    </nav>
  );
}

function MachineAvatar({
  machine,
  active,
  online,
  runningCount,
  onClick,
}: {
  machine: MachineProfile;
  active: boolean;
  online: boolean | null;
  runningCount: number;
  onClick: () => void;
}) {
  const removeMachine = useStore((s) => s.removeMachine);
  const [menu, setMenu] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);

  const dotColor =
    online === null ? 'bg-ink-faint' : online ? 'bg-success' : 'bg-ink-faint';
  return (
    <div className="relative">
      <button
        title={`${machine.name}${online === false ? '（离线）' : ''} — 右键管理`}
        aria-label={`${machine.name}${online === false ? '，离线' : online ? '，在线' : '，检测中'}`}
        onClick={onClick}
        onContextMenu={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setMenu(true);
        }}
        className={`rail-machine relative flex h-10 w-10 items-center justify-center rounded-[13px] text-[11px] font-semibold tracking-[0.04em] transition-all ${
          active
            ? 'rail-machine-active'
            : 'hover:-translate-y-px hover:bg-white/12'
        } ${online === false ? 'opacity-45' : ''}`}
      >
        {machineInitials(machine.name)}
        {active && <span className="rail-active-indicator" aria-hidden="true" />}
        <span
          className={`absolute -right-0.5 -bottom-0.5 h-2.5 w-2.5 rounded-full border-2 border-[var(--am-rail-bg)] ${dotColor}`}
        />
        {runningCount > 0 && (
          <span className="absolute -top-1.5 -right-1.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold text-on-accent">
            {runningCount}
          </span>
        )}
      </button>
      {menu && (
        <Menu onClose={() => setMenu(false)} className="top-0 left-full ml-2">
          <MenuItem
            danger
            icon={<IconTrash size={13} />}
            label="移除机器"
            onClick={() => {
              setMenu(false);
              setConfirmRemove(true);
            }}
          />
        </Menu>
      )}
      {confirmRemove && (
        <ConfirmDialog
          title={`移除机器「${machine.name}」？`}
          description="仅从这个客户端移除，不影响机器上的守护进程和会话数据。"
          confirmLabel="移除"
          onConfirm={() => {
            setConfirmRemove(false);
            void removeMachine(machine.id);
          }}
          onCancel={() => setConfirmRemove(false)}
        />
      )}
    </div>
  );
}
