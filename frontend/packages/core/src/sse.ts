import type { RenderState, StreamDelta, WireEvent } from './types.js';

export interface SseSubscribeOptions {
  /** Resume from this seq; only events with seq > afterSeq are delivered. Default 0. */
  afterSeq?: number;
  /** Called for every parsed `am_event` frame (drives the resume cursor). */
  onEvent: (event: WireEvent) => void;
  /** Called for every `am_render` snapshot (the transcript to display). */
  onRender?: (state: RenderState) => void;
  /** Called for every live `am_delta` frame (token-level preview; ephemeral). */
  onDelta?: (delta: StreamDelta) => void;
  /** Called on transport errors (before an auto-reconnect is scheduled). */
  onError?: (error: unknown) => void;
  /** Called when a reconnect is (re)established, with the seq we resume from. */
  onReconnect?: (afterSeq: number) => void;
}

export interface SseClientConfig {
  /** Absolute daemon base URL, e.g. "http://localhost:8888". */
  baseUrl: string;
  /** Bearer token; sent via query string because EventSource can't set headers. */
  token: string;
  /**
   * EventSource constructor. Defaults to the global one (browsers). In Node,
   * pass one from the `eventsource` package.
   */
  EventSource?: EventSourceCtor;
  /** Base delay for reconnect backoff, ms. Default 1000. */
  reconnectBaseDelayMs?: number;
  /** Max delay for reconnect backoff, ms. Default 15000. */
  reconnectMaxDelayMs?: number;
}

/** Minimal EventSource contract we rely on (browser + `eventsource` package). */
export interface EventSourceLike {
  addEventListener(type: string, listener: (ev: MessageEvent) => void): void;
  close(): void;
  onopen: ((ev: Event) => void) | null;
  onerror: ((ev: Event) => void) | null;
}
export type EventSourceCtor = new (url: string) => EventSourceLike;

function trimTrailingSlash(s: string): string {
  return s.endsWith('/') ? s.slice(0, -1) : s;
}

/**
 * SSE client for the per-session event stream.
 *
 * Connects to `${baseUrl}/api/sessions/:id/stream?token=..&after_seq=..`,
 * listens for the named `am_event` frames, and tracks the last seq so it can
 * auto-reconnect from `lastSeq` after a transport drop. Also honors the
 * server's named `reconnect` event ({ afterSeq }).
 */
export class SseClient {
  private readonly baseUrl: string;
  private readonly token: string;
  private readonly EventSourceImpl: EventSourceCtor;
  private readonly baseDelay: number;
  private readonly maxDelay: number;

  constructor(config: SseClientConfig) {
    this.baseUrl = trimTrailingSlash(config.baseUrl);
    this.token = config.token;
    const ctor = config.EventSource ?? (globalThis as { EventSource?: EventSourceCtor }).EventSource;
    if (!ctor) {
      throw new Error(
        'No EventSource available; pass one via config.EventSource (e.g. from the "eventsource" package in Node).',
      );
    }
    this.EventSourceImpl = ctor;
    this.baseDelay = config.reconnectBaseDelayMs ?? 1000;
    this.maxDelay = config.reconnectMaxDelayMs ?? 15000;
  }

  /**
   * Subscribe to a session's stream. Returns an unsubscribe function that
   * closes the connection and cancels any pending reconnect.
   */
  subscribe(sessionId: string, opts: SseSubscribeOptions): () => void {
    let lastSeq = opts.afterSeq ?? 0;
    let closed = false;
    let attempt = 0;
    let es: EventSourceLike | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    const clearTimer = () => {
      if (reconnectTimer != null) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    };

    const buildUrl = (afterSeq: number): string => {
      const q = new URLSearchParams();
      q.set('token', this.token);
      q.set('after_seq', String(afterSeq));
      return `${this.baseUrl}/api/sessions/${encodeURIComponent(sessionId)}/stream?${q.toString()}`;
    };

    const scheduleReconnect = () => {
      if (closed) return;
      clearTimer();
      const delay = Math.min(this.baseDelay * 2 ** attempt, this.maxDelay);
      attempt += 1;
      reconnectTimer = setTimeout(() => connect(lastSeq), delay);
    };

    const connect = (afterSeq: number) => {
      if (closed) return;
      clearTimer();
      const source = new this.EventSourceImpl(buildUrl(afterSeq));
      es = source;

      source.onopen = () => {
        attempt = 0; // reset backoff on a successful open
        opts.onReconnect?.(afterSeq);
      };

      source.addEventListener('am_event', (ev: MessageEvent) => {
        const event = parseWireEvent(ev.data);
        if (!event) return;
        if (event.seq > lastSeq) lastSeq = event.seq;
        opts.onEvent(event);
      });

      // Server-derived render snapshot (the transcript to display).
      source.addEventListener('am_render', (ev: MessageEvent) => {
        const rs = parseRender(ev.data);
        if (rs) opts.onRender?.(rs);
      });

      // Live-only token deltas: no seq, do not advance lastSeq / resume cursor.
      source.addEventListener('am_delta', (ev: MessageEvent) => {
        const delta = parseDelta(ev.data);
        if (delta) opts.onDelta?.(delta);
      });

      // Server-initiated resync after a dropped-subscriber overflow.
      source.addEventListener('reconnect', (ev: MessageEvent) => {
        const target = parseReconnectSeq(ev.data);
        if (target != null) lastSeq = target;
        source.close();
        if (es === source) es = null;
        connect(lastSeq);
      });

      source.onerror = (err: Event) => {
        opts.onError?.(err);
        source.close();
        if (es === source) es = null;
        scheduleReconnect();
      };
    };

    connect(lastSeq);

    return () => {
      closed = true;
      clearTimer();
      if (es) {
        es.close();
        es = null;
      }
    };
  }
}

function parseWireEvent(data: unknown): WireEvent | null {
  if (typeof data !== 'string') return null;
  try {
    const obj = JSON.parse(data) as WireEvent;
    if (obj && typeof obj.seq === 'number' && typeof obj.type === 'string') return obj;
    return null;
  } catch {
    return null;
  }
}

function parseRender(data: unknown): RenderState | null {
  if (typeof data !== 'string') return null;
  try {
    const obj = JSON.parse(data) as RenderState;
    if (obj && Array.isArray(obj.rows) && typeof obj.basedOnSeq === 'number') return obj;
    return null;
  } catch {
    return null;
  }
}

function parseDelta(data: unknown): StreamDelta | null {
  if (typeof data !== 'string') return null;
  try {
    const obj = JSON.parse(data) as StreamDelta;
    if (obj && typeof obj.text === 'string') return obj;
    return null;
  } catch {
    return null;
  }
}

function parseReconnectSeq(data: unknown): number | null {
  if (typeof data !== 'string') return null;
  try {
    const obj = JSON.parse(data) as { afterSeq?: unknown };
    return typeof obj.afterSeq === 'number' ? obj.afterSeq : null;
  } catch {
    return null;
  }
}
