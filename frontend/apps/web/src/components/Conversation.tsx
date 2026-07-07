import { useEffect, useMemo, useRef } from 'react';
import type {
  AssistantMessagePayload,
  ErrorPayload,
  RunFinishedPayload,
  ToolCallPayload,
  ToolResultPayload,
  UserMessagePayload,
  WireEvent,
} from '@agent-master/core';
import { useStore } from '../store.js';

export function Conversation() {
  const currentSessionId = useStore((s) => s.currentSessionId);
  const events = useStore((s) =>
    currentSessionId ? s.eventsBySession[currentSessionId] ?? [] : [],
  );
  const historyLoading = useStore((s) => s.historyLoading);
  const runActive = useStore((s) => s.runActive);
  const streamStatus = useStore((s) => s.streamStatus);
  const interrupt = useStore((s) => s.interrupt);

  const scrollRef = useRef<HTMLDivElement>(null);

  // Index tool_result output by tool id so we can attach it to its tool_call row.
  const resultById = useMemo(() => {
    const map = new Map<string, unknown>();
    for (const e of events) {
      if (e.type === 'tool_result') {
        const p = e.payload as ToolResultPayload;
        if (p?.id != null) map.set(p.id, p.output);
      }
    }
    return map;
  }, [events]);

  // Latest run_finished state, for the header pill.
  const lastFinished = useMemo(() => {
    for (let i = events.length - 1; i >= 0; i--) {
      const e = events[i];
      if (e?.type === 'run_finished') return (e.payload as RunFinishedPayload).state;
    }
    return null;
  }, [events]);

  // Auto-scroll to bottom on new events.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events.length]);

  return (
    <>
      <div className="main-header">
        <RunStatus runActive={runActive} lastFinished={lastFinished} />
        <StreamIndicator status={streamStatus} />
        <div className="spacer" />
        {runActive && (
          <button className="danger" onClick={() => void interrupt()}>
            Interrupt
          </button>
        )}
      </div>

      <div className="conversation" ref={scrollRef}>
        {historyLoading && events.length === 0 && <div className="empty">Loading history…</div>}
        {!historyLoading && events.length === 0 && (
          <div className="empty">No messages yet. Say something below.</div>
        )}
        {events.map((e) => (
          <EventRow key={e.seq} event={e} resultById={resultById} />
        ))}
      </div>
    </>
  );
}

function RunStatus({
  runActive,
  lastFinished,
}: {
  runActive: boolean;
  lastFinished: string | null;
}) {
  if (runActive) {
    return (
      <span className="run-pill running">
        <span className="dot pulse" />
        running…
      </span>
    );
  }
  if (lastFinished === 'done') {
    return (
      <span className="run-pill done">
        <span className="dot" />
        done
      </span>
    );
  }
  if (lastFinished === 'failed' || lastFinished === 'interrupted') {
    return (
      <span className={`run-pill ${lastFinished}`}>
        <span className="dot" />
        {lastFinished}
      </span>
    );
  }
  return <span className="run-pill idle">idle</span>;
}

/** Shows the SSE connection state only when it's not cleanly open. */
function StreamIndicator({ status }: { status: string }) {
  if (status === 'connecting') {
    return (
      <span className="run-pill" style={{ color: 'var(--warn)', borderColor: 'var(--warn)' }}>
        <span className="dot pulse" style={{ background: 'var(--warn)' }} />
        connecting…
      </span>
    );
  }
  if (status === 'error') {
    return (
      <span className="run-pill" style={{ color: 'var(--warn)', borderColor: 'var(--warn)' }}>
        <span className="dot pulse" style={{ background: 'var(--warn)' }} />
        reconnecting…
      </span>
    );
  }
  return null;
}

function EventRow({
  event,
  resultById,
}: {
  event: WireEvent;
  resultById: Map<string, unknown>;
}) {
  switch (event.type) {
    case 'user_message': {
      const p = event.payload as UserMessagePayload;
      return (
        <div className="bubble user">
          <div className="bubble-role">you</div>
          {p.text}
        </div>
      );
    }
    case 'assistant_message': {
      const p = event.payload as AssistantMessagePayload;
      return (
        <div className="bubble assistant">
          <div className="bubble-role">assistant</div>
          {p.text}
        </div>
      );
    }
    case 'tool_call': {
      const p = event.payload as ToolCallPayload;
      const output = resultById.get(p.id);
      const hasResult = resultById.has(p.id);
      return (
        <details className="tool">
          <summary>
            <span className="tool-badge">tool</span>
            <span className="tool-name">{p.name}</span>
            <span className="spacer" />
            <span className="status-line">{hasResult ? 'done' : 'running…'}</span>
          </summary>
          <pre>{formatValue(p.input)}</pre>
          {hasResult && (
            <pre style={{ borderTop: '1px dashed var(--border)' }}>
              → {formatValue(output)}
            </pre>
          )}
        </details>
      );
    }
    case 'tool_result':
      // Rendered inline with its tool_call above; skip standalone.
      return null;
    case 'run_started':
    case 'run_finished':
      // Reflected in the header pill; no inline row.
      return null;
    case 'error': {
      const p = event.payload as ErrorPayload;
      return <div className="bubble error">error: {p.message}</div>;
    }
    default:
      return null;
  }
}

function formatValue(v: unknown): string {
  if (v == null) return '';
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
