import type { RecentSession } from '@agent-master/core';

/**
 * Client-derived triage state for a session.
 * - running:   a run is active right now
 * - attention: the agent produced output (reply / failure) the user hasn't seen
 * - idle:      nothing new
 */
export type SessionStatus = 'running' | 'attention' | 'idle';

export function sessionStatus(s: RecentSession, seenSeq: number | undefined): SessionStatus {
  if (s.activeRunId) return 'running';
  if (s.lastSeq > (seenSeq ?? 0)) return 'attention';
  return 'idle';
}

/** One-line description of what the session needs / did, for list rows. */
export function statusLine(s: RecentSession, status: SessionStatus): string {
  if (status === 'running') return s.lastPreview || '正在运行…';
  if (status === 'attention') {
    if (s.lastRunState === 'failed') return s.lastPreview ? `运行失败：${s.lastPreview}` : '运行失败';
    if (s.lastRunState === 'interrupted') return '已中断，等待你的下一步指示';
    return s.lastPreview || '有新回复';
  }
  return s.lastPreview || '—';
}
