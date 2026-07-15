import type { RecentSession } from '@agent-master/core';
import { describe, expect, it } from 'vitest';
import { statusLine } from './status.js';

const session = (overrides: Partial<RecentSession>): RecentSession => ({
  id: 'session',
  title: 'Session',
  lastPreview: '',
  lastSeq: 1,
  workspaceDir: '/tmp/project',
  createdAt: '2026-07-15T00:00:00Z',
  updatedAt: '2026-07-15T00:00:00Z',
  ...overrides,
});

describe('session status line', () => {
  it('keeps the latest preview when an interrupted session needs attention', () => {
    expect(
      statusLine(
        session({
          lastPreview: '我已经整理好了修改建议',
          lastRunState: 'interrupted',
        }),
        'attention',
      ),
    ).toBe('我已经整理好了修改建议');
  });

  it('does not add a status line when an interrupted session has no preview', () => {
    expect(statusLine(session({ lastRunState: 'interrupted' }), 'attention')).toBe('');
  });
});
