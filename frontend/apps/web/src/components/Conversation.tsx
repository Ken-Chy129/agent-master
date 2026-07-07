import { useEffect, useRef } from 'react';
import type { RenderRow } from '@agent-master/core';
import { EMPTY_RENDER, useStore } from '../store.js';

export function Conversation() {
  const currentSessionId = useStore((s) => s.currentSessionId);
  const render = useStore((s) =>
    currentSessionId ? (s.renderBySession[currentSessionId] ?? EMPTY_RENDER) : EMPTY_RENDER,
  );
  const runActive = useStore((s) => s.runActive);
  const streamStatus = useStore((s) => s.streamStatus);
  const streamingText = useStore((s) => s.streamingText);
  const interrupt = useStore((s) => s.interrupt);

  const scrollRef = useRef<HTMLDivElement>(null);
  const rows = render.rows;

  // Auto-scroll on new rows and as the live preview grows.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [rows.length, streamingText]);

  const connecting = streamStatus === 'connecting' && rows.length === 0;

  return (
    <>
      <div className="main-header">
        <RunStatus runActive={runActive} lastRunState={render.lastRunState} />
        <StreamIndicator status={streamStatus} />
        <div className="spacer" />
        {runActive && (
          <button className="danger" onClick={() => void interrupt()}>
            Interrupt
          </button>
        )}
      </div>

      <div className="conversation" ref={scrollRef}>
        {connecting && <div className="empty">Loading…</div>}
        {!connecting && rows.length === 0 && (
          <div className="empty">No messages yet. Say something below.</div>
        )}
        {rows.map((row) => (
          <Row key={row.id} row={row} />
        ))}
        {streamingText && (
          <div className="bubble assistant streaming">
            <div className="bubble-role">assistant</div>
            {streamingText}
            <span className="stream-cursor">▌</span>
          </div>
        )}
      </div>
    </>
  );
}

function RunStatus({
  runActive,
  lastRunState,
}: {
  runActive: boolean;
  lastRunState?: string;
}) {
  if (runActive) {
    return (
      <span className="run-pill running">
        <span className="dot pulse" />
        running…
      </span>
    );
  }
  if (lastRunState === 'done') {
    return (
      <span className="run-pill done">
        <span className="dot" />
        done
      </span>
    );
  }
  if (lastRunState === 'failed' || lastRunState === 'interrupted') {
    return (
      <span className={`run-pill ${lastRunState}`}>
        <span className="dot" />
        {lastRunState}
      </span>
    );
  }
  return <span className="run-pill idle">idle</span>;
}

/** Shows the SSE connection state only when it's not cleanly open. */
function StreamIndicator({ status }: { status: string }) {
  if (status === 'connecting' || status === 'error') {
    const label = status === 'connecting' ? 'connecting…' : 'reconnecting…';
    return (
      <span className="run-pill" style={{ color: 'var(--warn)', borderColor: 'var(--warn)' }}>
        <span className="dot pulse" style={{ background: 'var(--warn)' }} />
        {label}
      </span>
    );
  }
  return null;
}

/** Dumb-renders one server-derived row. Tool pairing/status is already done server-side. */
function Row({ row }: { row: RenderRow }) {
  switch (row.kind) {
    case 'user':
      return (
        <div className="bubble user">
          <div className="bubble-role">you</div>
          {row.text}
        </div>
      );
    case 'assistant':
      return (
        <div className="bubble assistant">
          <div className="bubble-role">assistant</div>
          {row.text}
        </div>
      );
    case 'tool':
      return (
        <details className="tool">
          <summary>
            <span className="tool-badge">tool</span>
            <span className="tool-name">{row.name}</span>
            <span className="spacer" />
            <span className="status-line">{row.status === 'done' ? 'done' : 'running…'}</span>
          </summary>
          <pre>{formatValue(row.input)}</pre>
          {row.output !== undefined && row.output !== null && (
            <pre style={{ borderTop: '1px dashed var(--border)' }}>→ {formatValue(row.output)}</pre>
          )}
        </details>
      );
    case 'error':
      return <div className="bubble error">error: {row.text}</div>;
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
