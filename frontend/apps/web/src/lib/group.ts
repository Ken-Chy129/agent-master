import type { RecentSession } from '@agent-master/core';

/** How the session column groups its list. */
export type GroupMode = 'project' | 'updated' | 'created';

export const GROUP_MODES: { value: GroupMode; label: string }[] = [
  { value: 'project', label: '按项目' },
  { value: 'updated', label: '按更新时间' },
  { value: 'created', label: '按创建时间' },
];

export interface SessionGroup {
  /** Stable key for collapse state (dir path or time bucket id). */
  key: string;
  /** Display label (project basename or time bucket name). */
  label: string;
  /** Full workspace dir (project mode only — powers tooltip + quick create). */
  dir?: string;
  sessions: RecentSession[];
}

/** Last path segment as the project name, e.g. /root/apps/agent-master -> agent-master. */
export function projectName(dir: string): string {
  const seg = dir.replace(/\/+$/, '').split('/').filter(Boolean);
  return seg[seg.length - 1] || dir || '未知目录';
}

function timeBucket(iso: string): { key: string; label: string; order: number } {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return { key: 'older', label: '更早', order: 4 };
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  if (t >= startOfToday) return { key: 'today', label: '今天', order: 0 };
  if (t >= startOfToday - 86_400_000) return { key: 'yesterday', label: '昨天', order: 1 };
  if (t >= startOfToday - 6 * 86_400_000) return { key: 'week', label: '本周', order: 2 };
  if (t >= startOfToday - 29 * 86_400_000) return { key: 'month', label: '本月', order: 3 };
  return { key: 'older', label: '更早', order: 4 };
}

/**
 * Group sessions for the session column. Groups come back in display order;
 * sessions inside each group are newest-updated first.
 */
export function groupSessions(sessions: RecentSession[], mode: GroupMode): SessionGroup[] {
  const byUpdated = (a: RecentSession, b: RecentSession) =>
    b.updatedAt.localeCompare(a.updatedAt);

  if (mode === 'project') {
    const map = new Map<string, SessionGroup>();
    for (const s of sessions) {
      const dir = s.workspaceDir || '';
      let g = map.get(dir);
      if (!g) {
        g = { key: dir || 'unknown', label: projectName(dir), dir, sessions: [] };
        map.set(dir, g);
      }
      g.sessions.push(s);
    }
    const groups = [...map.values()];
    for (const g of groups) g.sessions.sort(byUpdated);
    // Most recently active project first.
    groups.sort((a, b) => b.sessions[0]!.updatedAt.localeCompare(a.sessions[0]!.updatedAt));
    return groups;
  }

  const pick = mode === 'created' ? (s: RecentSession) => s.createdAt : (s: RecentSession) => s.updatedAt;
  const map = new Map<string, SessionGroup & { order: number }>();
  for (const s of sessions) {
    const b = timeBucket(pick(s));
    let g = map.get(b.key);
    if (!g) {
      g = { key: b.key, label: b.label, sessions: [], order: b.order };
      map.set(b.key, g);
    }
    g.sessions.push(s);
  }
  const groups = [...map.values()].sort((a, b) => a.order - b.order);
  for (const g of groups) {
    g.sessions.sort(
      mode === 'created' ? (a, b) => b.createdAt.localeCompare(a.createdAt) : byUpdated,
    );
  }
  return groups;
}
