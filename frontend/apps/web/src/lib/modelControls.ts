import type { ModelInfo } from '@agent-master/core';

export type ModelCatalogStatus = 'idle' | 'loading' | 'ready' | 'unavailable';

export interface ModelControlState {
  disabled: boolean;
  label: string;
  options: ModelInfo[];
  value: string;
}

/** Derive an honest model picker state without pretending old daemons support it. */
export function modelControlState(
  status: ModelCatalogStatus,
  models: ModelInfo[],
  selectedValue: string,
): ModelControlState {
  if (status !== 'ready' || models.length === 0) {
    return {
      disabled: true,
      label: status === 'unavailable' ? '模型不可用' : '加载模型…',
      options: [],
      value: '',
    };
  }

  const selected = models.find((model) => model.id === selectedValue);
  const fallback = models.find((model) => model.id === '') ?? models[0]!;
  const resolved = selected ?? fallback;
  return {
    disabled: false,
    label: resolved.label,
    options: models,
    value: resolved.id,
  };
}
