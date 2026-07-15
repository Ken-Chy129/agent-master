import type { ModelInfo } from '@agent-master/core';
import { describe, expect, it } from 'vitest';
import { modelControlState } from './modelControls.js';

const models: ModelInfo[] = [
  { id: '', label: '默认模型' },
  { id: 'sonnet', label: 'Sonnet', efforts: ['low', 'high'] },
];

describe('modelControlState', () => {
  it('does not expose fallback choices when the daemon lacks model support', () => {
    expect(modelControlState('unavailable', [], 'sonnet')).toEqual({
      disabled: true,
      label: '模型不可用',
      options: [],
      value: '',
    });
  });

  it('keeps the exact provider model id when the catalog is ready', () => {
    expect(modelControlState('ready', models, 'sonnet')).toMatchObject({
      disabled: false,
      label: 'Sonnet',
      value: 'sonnet',
    });
  });

  it('falls back to the provider default when a stored model is no longer listed', () => {
    expect(modelControlState('ready', models, 'removed-model')).toMatchObject({
      disabled: false,
      label: '默认模型',
      value: '',
    });
  });
});
