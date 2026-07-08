import { useEffect, useMemo, useRef, useState } from 'react';
import type { RenderRow } from '@agent-master/core';
import { copyText } from '../lib/copy.js';
import { hhmm } from '../lib/time.js';
import { EMPTY_RENDER, useStore } from '../store.js';
import { IconCheck, IconChevronRight, IconCopy, IconPanelLeft, IconTerminal } from './icons.js';
import { Markdown } from './Markdown.js';

/** Feed item: a plain row, or a run of consecutive tool rows folded together. */
type FeedItem =
  | { kind: 'row'; row: RenderRow }
  | { kind: 'tools'; id: string; rows: RenderRow[] };

function groupRows(rows: RenderRow[]): FeedItem[] {
  const items: FeedItem[] = [];
  for (const row of rows) {
    const last = items[items.length - 1];
    if (row.kind === 'tool') {
      if (last?.kind === 'tools') last.rows.push(row);
      else items.push({ kind: 'tools', id: row.id, rows: [row] });
    } else {
      items.push({ kind: 'row', row });
    }
  }
  return items;
}

export function Conversation() {
  const currentSessionId = useStore((s) => s.currentSessionId);
  const currentSessionMeta = useStore((s) => s.currentSessionMeta);
  const render = useStore((s) =>
    currentSessionId ? (s.renderBySession[currentSessionId] ?? EMPTY_RENDER) : EMPTY_RENDER,
  );
  const runActive = useStore((s) => s.runActive);
  const streamStatus = useStore((s) => s.streamStatus);
  const streamingText = useStore((s) => s.streamingText);

  const scrollRef = useRef<HTMLDivElement>(null);
  // Stick-to-bottom autoscroll: follow new content only while the user is
  // already near the bottom, so reading history during a run isn't hijacked.
  const stickRef = useRef(true);

  const items = useMemo(() => groupRows(render.rows), [render.rows]);

  // The copy+time footer belongs to the END of each exchange: the last
  // assistant message before the next user message. The feed's trailing
  // assistant message only counts once the run has settled.
  const turnEnds = useMemo(() => {
    const ends = new Set<string>();
    let lastAssistant: string | null = null;
    for (const it of items) {
      if (it.kind !== 'row') continue;
      if (it.row.kind === 'user') {
        if (lastAssistant) ends.add(lastAssistant);
        lastAssistant = null;
      } else if (it.row.kind === 'assistant') {
        lastAssistant = it.row.id;
      }
    }
    if (lastAssistant && !runActive) ends.add(lastAssistant);
    return ends;
  }, [items, runActive]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickRef.current) el.scrollTop = el.scrollHeight;
  }, [render.basedOnSeq, streamingText, currentSessionId]);

  const onScroll = () => {
    const el = scrollRef.current;
    if (el) stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };

  const columnCollapsed = useStore((s) => s.sessionColumnCollapsed);
  const toggleColumn = useStore((s) => s.toggleSessionColumn);

  const connecting = streamStatus === 'connecting' && render.rows.length === 0;

  return (
    <>
      <header className="app-drag flex items-center gap-3 border-b border-border bg-surface px-4 py-2.5">
        {columnCollapsed && (
          <button
            title="展开会话列表"
            onClick={toggleColumn}
            className="-ml-1 flex-none rounded-md p-1 text-ink-faint hover:bg-raised hover:text-ink"
          >
            <IconPanelLeft size={15} />
          </button>
        )}
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
          <StreamIndicator status={streamStatus} />
        </div>
      </header>

      <div ref={scrollRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto px-5 pt-6 pb-10 [scrollbar-gutter:stable_both-edges]">
        <div className="mx-auto flex max-w-[52rem] flex-col gap-4">
          {connecting && <div className="py-10 text-center text-sm text-ink-muted">加载中…</div>}
          {!connecting && render.rows.length === 0 && (
            <div className="py-14 text-center">
              <div className="text-sm font-medium">开始一个任务</div>
              <p className="mt-1 text-xs text-ink-muted">
                在下方描述要做的事，agent 会在这个工作目录里执行。
              </p>
            </div>
          )}
          {items.map((item) =>
            item.kind === 'tools' ? (
              <ToolGroup key={item.id} rows={item.rows} />
            ) : (
              <Row key={item.row.id} row={item.row} turnEnd={turnEnds.has(item.row.id)} />
            ),
          )}
          {streamingText && (
            <div className="max-w-[95%] self-start">
              <Markdown text={streamingText} />
              <span className="stream-cursor">▌</span>
            </div>
          )}
        </div>
      </div>
    </>
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

function Row({ row, turnEnd }: { row: RenderRow; turnEnd: boolean }) {
  switch (row.kind) {
    case 'user':
      return <UserRow text={row.text ?? ''} createdAt={row.createdAt} />;
    case 'assistant':
      return <AssistantRow text={row.text ?? ''} createdAt={row.createdAt} turnEnd={turnEnd} />;
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

/** Small copy button used in message meta rows. */
function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={() => {
        void copyText(text).then((ok) => {
          if (ok) {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          }
        });
      }}
      className="flex flex-none items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-ink-faint hover:bg-raised hover:text-ink"
    >
      {copied ? <IconCheck size={12} className="text-success" /> : <IconCopy size={12} />}
      {copied ? '已复制' : '复制'}
    </button>
  );
}

function UserRow({ text, createdAt }: { text: string; createdAt?: string }) {
  return (
    <div className="group flex max-w-[75%] flex-col items-end self-end">
      <div className="rounded-2xl rounded-br-md bg-raised px-3.5 py-2 text-base leading-[1.7] whitespace-pre-wrap">
        {text}
      </div>
      {/* In-flow so the gap is reserved and the hover reveal never overlaps
          the next message. */}
      <div className="mt-0.5 flex h-5 items-center gap-2 whitespace-nowrap opacity-0 transition-opacity group-hover:opacity-100">
        {createdAt && <span className="flex-none text-[11px] text-ink-faint">{hhmm(createdAt)}</span>}
        <CopyButton text={text} />
      </div>
    </div>
  );
}

function AssistantRow({
  text,
  createdAt,
  turnEnd,
}: {
  text: string;
  createdAt?: string;
  turnEnd: boolean;
}) {
  return (
    <div className="max-w-[95%] self-start">
      <Markdown text={text} />
      {/* Only the exchange's final message carries the footer, as a quiet
          always-visible end-of-turn marker. */}
      {turnEnd && (
        <div className="mt-1.5 -ml-1.5 flex items-center gap-2 whitespace-nowrap">
          <CopyButton text={text} />
          {createdAt && <span className="flex-none text-[11px] text-ink-faint">{hhmm(createdAt)}</span>}
        </div>
      )}
    </div>
  );
}

/** A run of consecutive tool calls, folded into one quiet expandable line. */
function ToolGroup({ rows }: { rows: RenderRow[] }) {
  const [open, setOpen] = useState(false);
  const running = rows.some((r) => r.status !== 'done');

  const summary = useMemo(() => {
    const names: string[] = [];
    for (const r of rows) {
      const n = r.name ?? 'tool';
      if (!names.includes(n)) names.push(n);
    }
    const shown = names.slice(0, 4).join('、');
    return names.length > 4 ? `${shown} 等` : shown;
  }, [rows]);

  return (
    <div className="w-full max-w-[95%] self-start">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-1.5 rounded-lg px-2 py-1 text-left text-xs text-ink-muted transition-colors hover:bg-raised"
      >
        <IconTerminal size={13} className="flex-none text-ink-faint" />
        <span className="min-w-0 flex-1 truncate">
          {running ? '正在执行' : '执行了'} {rows.length} 个操作
          <span className="mx-1.5 text-ink-faint">·</span>
          <span className="font-mono text-[11px]">{summary}</span>
        </span>
        {running && (
          <span className="pulse-dot h-1.5 w-1.5 flex-none rounded-full bg-accent" />
        )}
        <IconChevronRight
          size={12}
          className={`flex-none text-ink-faint transition-transform ${open ? 'rotate-90' : ''}`}
        />
      </button>
      {open && (
        <div className="mt-1 ml-[7px] space-y-1 border-l border-border pl-3">
          {rows.map((r) => (
            <ToolDetail key={r.id} row={r} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolDetail({ row }: { row: RenderRow }) {
  return (
    <details className="overflow-hidden rounded-lg border border-border bg-surface">
      <summary className="flex cursor-pointer items-center gap-2 px-2.5 py-1.5 text-xs select-none [&::-webkit-details-marker]:hidden">
        <span className="font-mono font-medium">{row.name}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-faint">
          {inputPreview(row.input)}
        </span>
        {row.status === 'done' ? (
          <IconCheck size={12} className="flex-none text-success" />
        ) : (
          <span className="pulse-dot h-1.5 w-1.5 flex-none rounded-full bg-accent" />
        )}
      </summary>
      <pre className="max-h-64 overflow-x-auto border-t border-border px-2.5 py-2 font-mono text-[11.5px] leading-relaxed text-ink-muted">
        {formatValue(row.input)}
      </pre>
      {row.output !== undefined && row.output !== null && (
        <pre className="max-h-64 overflow-x-auto border-t border-dashed border-border px-2.5 py-2 font-mono text-[11.5px] leading-relaxed text-ink-muted">
          {formatValue(row.output)}
        </pre>
      )}
    </details>
  );
}

/** One-line human-scannable preview of a tool's input. */
function inputPreview(v: unknown): string {
  let s = '';
  if (typeof v === 'string') s = v;
  else if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>;
    // Common single-field tool inputs read much better than raw JSON.
    const key = ['command', 'file_path', 'path', 'pattern', 'query', 'url'].find(
      (k) => typeof o[k] === 'string',
    );
    s = key ? (o[key] as string) : JSON.stringify(v);
  } else if (v != null) s = String(v);
  s = s.replace(/\s+/g, ' ').trim();
  return s.length > 90 ? `${s.slice(0, 90)}…` : s;
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
