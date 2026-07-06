export * from './types.js';
export { ApiClient, ApiError } from './api.js';
export type { ApiClientConfig } from './api.js';
export { SseClient } from './sse.js';
export type {
  SseClientConfig,
  SseSubscribeOptions,
  EventSourceLike,
  EventSourceCtor,
} from './sse.js';

import type {
  AssistantMessagePayload,
  ErrorPayload,
  RunFinishedPayload,
  RunStartedPayload,
  ToolCallPayload,
  ToolResultPayload,
  UserMessagePayload,
  WireEvent,
} from './types.js';

/**
 * Narrow a WireEvent's `payload` for a known event `type`. This is a convenience
 * for dumb renderers; it trusts the daemon's contract rather than validating.
 */
export function payloadOf(event: WireEvent & { type: 'user_message' }): UserMessagePayload;
export function payloadOf(event: WireEvent & { type: 'assistant_message' }): AssistantMessagePayload;
export function payloadOf(event: WireEvent & { type: 'tool_call' }): ToolCallPayload;
export function payloadOf(event: WireEvent & { type: 'tool_result' }): ToolResultPayload;
export function payloadOf(event: WireEvent & { type: 'run_started' }): RunStartedPayload;
export function payloadOf(event: WireEvent & { type: 'run_finished' }): RunFinishedPayload;
export function payloadOf(event: WireEvent & { type: 'error' }): ErrorPayload;
export function payloadOf(event: WireEvent): unknown;
export function payloadOf(event: WireEvent): unknown {
  return event.payload;
}
