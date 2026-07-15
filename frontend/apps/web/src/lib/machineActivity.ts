import type { RecentSession } from '@agent-master/core';
import { sessionStatus } from './status.js';

export interface MachineActivityCounts {
  attention: number;
  running: number;
}

export type MachineBadge = {
  count: number;
  kind: 'attention' | 'running';
  label: string;
} | null;

export function machineActivityCounts(
  sessions: RecentSession[],
  seenSeq: Record<string, number>,
): MachineActivityCounts {
  let attention = 0;
  let running = 0;
  for (const session of sessions) {
    const status = sessionStatus(session, seenSeq[session.id]);
    if (status === 'attention') attention += 1;
    else if (status === 'running') running += 1;
  }
  return { attention, running };
}

/** A single compact badge: actionable results take priority over live work. */
export function machineBadge(counts: MachineActivityCounts): MachineBadge {
  if (counts.attention > 0) {
    return {
      count: counts.attention,
      kind: 'attention',
      label: `${counts.attention} 个会话待处理`,
    };
  }
  if (counts.running > 0) {
    return {
      count: counts.running,
      kind: 'running',
      label: `${counts.running} 个会话运行中`,
    };
  }
  return null;
}
