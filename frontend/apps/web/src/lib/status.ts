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

/**
 * Flatten markdown syntax out of a preview snippet — previews come from raw
 * assistant text, and `## 结论` / `**bold**` read as noise in list rows.
 */
export function stripMarkdown(s: string): string {
  return s
    .replace(/```[\s\S]*?(```|$)/g, ' ') // fenced code blocks
    .replace(/`([^`]*)`/g, '$1') // inline code
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1') // images
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1') // links
    .replace(/^#{1,6}\s+/gm, '') // headings
    .replace(/(\*\*|__)(.*?)\1/g, '$2') // bold
    .replace(/(^|\s)[*_]([^*_]+)[*_]/g, '$1$2') // italics
    .replace(/^\s*>\s?/gm, '') // blockquotes
    .replace(/^\s*[-*+]\s+/gm, '') // list bullets
    .replace(/\s+/g, ' ')
    .trim();
}

/** One-line description of what the session needs / did, for list rows. */
export function statusLine(s: RecentSession, status: SessionStatus): string {
  const preview = stripMarkdown(s.lastPreview);
  if (status === 'running') return preview || '正在运行…';
  if (status === 'attention') {
    if (s.lastRunState === 'failed') return preview ? `运行失败：${preview}` : '运行失败';
    if (s.lastRunState === 'interrupted') return preview;
    return preview || '有新回复';
  }
  return preview;
}
