import { useEffect, useRef } from 'react';
import type { RenderRow } from '@agent-master/core';
import { EMPTY_RENDER, useStore } from '../store.js';
import { IconStop, IconTerminal } from './icons.js';

export function Conversation() {
  const currentSessionId = useStore((s) => s.currentSessionId);
  const currentSessionMeta = useStore((s) => s.currentSessionMeta);
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
      <header className="flex items-center gap-3 border-b border-border bg-surface px-4 py-2.5">
        <div className="min-w-0">
          <div className="truncate text-[13px] font-semibold">
            {currentSessionMeta?.title || '会话'}
          </div>
          <div className="truncate font-mono text-[10.5px] text-ink-faint">
            {currentSessionMeta
              ? `${currentSessionMeta.workspaceDir}${
                  currentSessionMeta.model ? ` · ${currentSessionMeta.model}` : ''
                }`
              : '…'}
          </div>
        </div>
        <div className="ml-auto flex flex-none items-center gap-2">
          <StatusPill runActive={runActive} lastRunState={render.lastRunState} />
          <StreamIndicator status={streamStatus} />
          {runActive && (
            <button
              onClick={() => void interrupt()}
              className="flex items-center gap-1.5 rounded-lg border border-danger/50 px-2.5 py-1 text-xs text-danger transition-colors hover:bg-danger-soft"
            >
              <IconStop size={12} />
              中断
            </button>
          )}
        </div>
      </header>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        <div className="mx-auto flex max-w-2xl flex-col gap-3">
          {connecting && <div className="py-8 text-center text-sm text-ink-muted">加载中…</div>}
          {!connecting && rows.length === 0 && (
            <div className="py-8 text-center text-sm text-ink-muted">
              还没有消息，在下方开始对话。
            </div>
          )}
          {rows.map((row) => (
            <Row key={row.id} row={row} />
          ))}
          {streamingText && (
            <div className="max-w-[92%] self-start text-sm leading-relaxed whitespace-pre-wrap opacity-90">
              {streamingText}
              <span className="stream-cursor">▌</span>
            </div>
          )}
        </div>
      </div>
    </>
  );
}

function StatusPill({
  runActive,
  lastRunState,
}: {
  runActive: boolean;
  lastRunState?: string;
}) {
  if (runActive) {
    return (
      <span className="flex items-center gap-1.5 rounded-full bg-accent-soft px-2.5 py-0.5 text-[11px] text-accent">
        <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-accent" />
        运行中
      </span>
    );
  }
  if (lastRunState === 'done') {
    return (
      <span className="flex items-center gap-1.5 rounded-full bg-success-soft px-2.5 py-0.5 text-[11px] text-success">
        <span className="h-1.5 w-1.5 rounded-full bg-success" />
        已完成
      </span>
    );
  }
  if (lastRunState === 'failed' || lastRunState === 'interrupted') {
    return (
      <span className="flex items-center gap-1.5 rounded-full bg-danger-soft px-2.5 py-0.5 text-[11px] text-danger">
        <span className="h-1.5 w-1.5 rounded-full bg-danger" />
        {lastRunState === 'failed' ? '运行失败' : '已中断'}
      </span>
    );
  }
  return (
    <span className="rounded-full border border-border px-2.5 py-0.5 text-[11px] text-ink-faint">
      空闲
    </span>
  );
}

/** Shows the SSE connection state only when it's not cleanly open. */
function StreamIndicator({ status }: { status: string }) {
  if (status !== 'connecting' && status !== 'error') return null;
  return (
    <span className="flex items-center gap-1.5 rounded-full bg-warn-soft px-2.5 py-0.5 text-[11px] text-warn">
      <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-warn-solid" />
      {status === 'connecting' ? '连接中' : '重连中'}
    </span>
  );
}

/** Dumb-renders one server-derived row. Tool pairing/status is already done server-side. */
function Row({ row }: { row: RenderRow }) {
  switch (row.kind) {
    case 'user':
      return (
        <div className="max-w-[85%] self-end rounded-2xl rounded-br-md bg-accent-soft px-3.5 py-2 text-sm leading-relaxed whitespace-pre-wrap">
          {row.text}
        </div>
      );
    case 'assistant':
      return (
        <div className="max-w-[92%] self-start text-sm leading-relaxed whitespace-pre-wrap">
          {row.text}
        </div>
      );
    case 'tool':
      return (
        <details className="group self-stretch overflow-hidden rounded-xl border border-border bg-surface">
          <summary className="flex cursor-pointer items-center gap-2 px-3 py-2 text-xs select-none [&::-webkit-details-marker]:hidden">
            <IconTerminal size={13} className="flex-none text-ink-faint" />
            <span className="font-mono font-medium">{row.name}</span>
            <span className="ml-auto flex-none">
              {row.status === 'done' ? (
                <span className="text-ink-faint">完成</span>
              ) : (
                <span className="flex items-center gap-1.5 text-accent">
                  <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-accent" />
                  运行中
                </span>
              )}
            </span>
          </summary>
          <pre className="max-h-72 overflow-x-auto border-t border-border px-3 py-2.5 font-mono text-[11.5px] leading-relaxed text-ink-muted">
            {formatValue(row.input)}
          </pre>
          {row.output !== undefined && row.output !== null && (
            <pre className="max-h-72 overflow-x-auto border-t border-dashed border-border px-3 py-2.5 font-mono text-[11.5px] leading-relaxed text-ink-muted">
              → {formatValue(row.output)}
            </pre>
          )}
        </details>
      );
    case 'error':
      return (
        <div className="self-center rounded-lg border border-danger/50 bg-danger-soft px-3 py-1.5 text-xs text-danger">
          {row.text}
        </div>
      );
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
