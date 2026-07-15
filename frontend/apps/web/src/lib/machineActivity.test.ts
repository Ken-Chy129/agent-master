import type { RecentSession } from '@agent-master/core';
import { describe, expect, it } from 'vitest';
import { machineActivityCounts, machineBadge } from './machineActivity.js';

const session = (overrides: Partial<RecentSession>): RecentSession => ({
  id: 'session',
  title: 'Session',
  lastPreview: '',
  lastSeq: 0,
  workspaceDir: '/tmp/project',
  createdAt: '2026-07-15T00:00:00Z',
  updatedAt: '2026-07-15T00:00:00Z',
  ...overrides,
});

describe('machine activity badge', () => {
  it('counts running and unseen completed sessions separately', () => {
    const counts = machineActivityCounts(
      [
        session({ id: 'running', activeRunId: 'run-1', lastSeq: 3 }),
        session({ id: 'attention', lastSeq: 5 }),
        session({ id: 'seen', lastSeq: 2 }),
      ],
      { attention: 4, seen: 2 },
    );

    expect(counts).toEqual({ attention: 1, running: 1 });
  });

  it('prioritizes the needs-attention count over the running count', () => {
    expect(machineBadge({ attention: 3, running: 2 })).toEqual({
      count: 3,
      kind: 'attention',
      label: '3 个会话待处理',
    });
  });

  it('shows the running count when nothing needs attention', () => {
    expect(machineBadge({ attention: 0, running: 2 })).toEqual({
      count: 2,
      kind: 'running',
      label: '2 个会话运行中',
    });
  });
});
