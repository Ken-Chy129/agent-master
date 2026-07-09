import { useEffect, useMemo, useRef, useState } from 'react';
import type { RenderRow } from '@agent-master/core';
import { copyText } from '../lib/copy.js';
import { hhmm } from '../lib/time.js';
import { useEscape } from '../lib/useEscape.js';
import { EMPTY_RENDER, useStore } from '../store.js';
import type { ImageRef } from '@agent-master/core';
import { Composer } from './Composer.js';
import {
  IconCheck,
  IconChevronRight,
  IconCopy,
  IconImage,
  IconPanelLeft,
  IconTerminal,
  IconX,
} from './icons.js';
import { Markdown } from './Markdown.js';

/** Feed item: a plain row, or a run of consecutive tool rows folded together. */
type FeedItem =
  | { kind: 'row'; row: RenderRow }
  | { kind: 'tools'; id: string; rows: RenderRow[] };

/** Compact elapsed-time label, e.g. "8s" or "6m 7s". */
function fmtElapsed(s: number): string {
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** Collapse user messages taller than this (px) behind a 展开/收起 toggle. */
const USER_MSG_COLLAPSE_PX = 288;

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
  const currentSessionMachineId = useStore((s) => s.currentSessionMachineId);
  const machine = useStore((s) =>
    currentSessionMachineId ? (s.machines.find((m) => m.id === currentSessionMachineId) ?? null) : null,
  );
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

  // The copy+time footer marks the END of each settled exchange. It carries the
  // exchange's final assistant text (for copy) and time, but is anchored to the
  // exchange's LAST feed item — normally that assistant message, but a trailing
  // tool group when the run was interrupted mid-tool. Anchoring to the last item
  // keeps the footer at the visual bottom instead of stranding a leftover tool
  // row beneath it. Keyed by anchor item id → footer content.
  const turnFooters = useMemo(() => {
    const map = new Map<string, { text: string; createdAt?: string }>();
    let lastAssistant: RenderRow | null = null;
    let anchorId: string | null = null;
    const flush = () => {
      if (lastAssistant && anchorId) {
        map.set(anchorId, { text: lastAssistant.text ?? '', createdAt: lastAssistant.createdAt });
      }
      lastAssistant = null;
    };
    for (const it of items) {
      if (it.kind === 'tools') {
        anchorId = it.id;
        continue;
      }
      if (it.row.kind === 'user') {
        flush(); // close the previous exchange before this new user turn
        anchorId = it.row.id;
      } else if (it.row.kind === 'assistant') {
        lastAssistant = it.row;
        anchorId = it.row.id;
      } else {
        anchorId = it.row.id;
      }
    }
    // The trailing exchange only settles once the run is done.
    if (!runActive) flush();
    return map;
  }, [items, runActive]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickRef.current) el.scrollTop = el.scrollHeight;
  }, [render.basedOnSeq, streamingText, currentSessionId]);

  // Elapsed seconds for the active run, so the "thinking" indicator shows the
  // agent is working during the gap before its first output.
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!runActive) {
      setElapsed(0);
      return;
    }
    const start = Date.now();
    const id = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 1000);
    return () => clearInterval(id);
  }, [runActive]);

  const onScroll = () => {
    const el = scrollRef.current;
    if (el) stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };

  const columnCollapsed = useStore((s) => s.sessionColumnCollapsed);
  const toggleColumn = useStore((s) => s.toggleSessionColumn);

  const connecting = streamStatus === 'connecting' && render.rows.length === 0;

  // Show a "thinking" hint while a run is active but nothing is streaming and
  // the tail isn't a running tool (which already shows its own activity) — i.e.
  // the gap after send before the agent's first visible output.
  const lastRow = render.rows[render.rows.length - 1];
  const tailRunningTool = lastRow?.kind === 'tool' && lastRow.status === 'running';
  const thinking = runActive && !streamingText && !tailRunningTool;

  // Full-size preview shown in an in-app lightbox (not a browser tab).
  const [lightbox, setLightbox] = useState<string | null>(null);
  useEscape(() => setLightbox(null), lightbox !== null);

  // Build an authenticated URL for a staged image (token in query so <img> can
  // load it without an Authorization header).
  const imgSrc = (file?: string): string | null => {
    if (!file || !machine || !currentSessionId) return null;
    return `${machine.baseUrl}/api/sessions/${encodeURIComponent(currentSessionId)}/uploads/${encodeURIComponent(file)}?token=${encodeURIComponent(machine.token)}`;
  };

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

      {/* One full-height scroll area: its scrollbar runs the whole panel in its
          own column (never covered), and the composer sticks to the bottom as
          in-flow content — messages scroll cleanly under its opaque bar. */}
      <div ref={scrollRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex min-h-full flex-col">
          <div className="flex-1">
            <div className="mx-auto flex max-w-[52rem] flex-col gap-4 px-5 pt-6">
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
                  <ToolGroup key={item.id} rows={item.rows} footer={turnFooters.get(item.id)} />
                ) : (
                  <Row
                    key={item.row.id}
                    row={item.row}
                    footer={turnFooters.get(item.row.id)}
                    imgSrc={imgSrc}
                    onImageOpen={setLightbox}
                  />
                ),
              )}
              {streamingText && (
                <div className="max-w-[95%] self-start">
                  <Markdown text={streamingText} />
                  <span className="stream-cursor">▌</span>
                </div>
              )}
              {thinking && (
                <div className="flex items-center gap-2 self-start py-1 text-sm text-ink-muted">
                  <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-accent" />
                  <span>正在思考{elapsed > 0 ? ` · ${fmtElapsed(elapsed)}` : '…'}</span>
                </div>
              )}
            </div>
          </div>
          <div className="sticky bottom-0 bg-canvas">
            <Composer />
          </div>
        </div>
      </div>

      {lightbox && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-6"
          onClick={() => setLightbox(null)}
        >
          <img
            src={lightbox}
            alt=""
            onClick={(e) => e.stopPropagation()}
            className="max-h-[90vh] max-w-[90vw] rounded-lg object-contain shadow-2xl"
          />
          <button
            onClick={() => setLightbox(null)}
            title="关闭"
            className="absolute top-4 right-4 flex h-9 w-9 items-center justify-center rounded-full bg-black/40 text-white/90 hover:bg-black/60"
          >
            <IconX size={18} />
          </button>
        </div>
      )}
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

/** End-of-turn footer content: the exchange's final assistant text + time. */
type TurnFooterData = { text: string; createdAt?: string };

function Row({
  row,
  footer,
  imgSrc,
  onImageOpen,
}: {
  row: RenderRow;
  footer?: TurnFooterData;
  imgSrc: (file?: string) => string | null;
  onImageOpen: (src: string) => void;
}) {
  switch (row.kind) {
    case 'user':
      return (
        <UserRow
          text={row.text ?? ''}
          images={row.images}
          createdAt={row.createdAt}
          imgSrc={imgSrc}
          onImageOpen={onImageOpen}
        />
      );
    case 'assistant':
      return <AssistantRow text={row.text ?? ''} footer={footer} />;
    case 'error':
      return (
        <div className="max-w-[90%] self-center rounded-lg border border-danger/50 bg-danger-soft px-3 py-1.5 text-xs text-danger [overflow-wrap:anywhere]">
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

function UserRow({
  text,
  images,
  createdAt,
  imgSrc,
  onImageOpen,
}: {
  text: string;
  images?: ImageRef[];
  createdAt?: string;
  imgSrc: (file?: string) => string | null;
  onImageOpen: (src: string) => void;
}) {
  // Cap tall messages (e.g. a pasted blob) with an expand/collapse toggle.
  const bodyRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [clamped, setClamped] = useState(false);
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    // scrollHeight reports full content height even while max-h clips it.
    const measure = () => setClamped(el.scrollHeight > USER_MSG_COLLAPSE_PX + 4);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [text]);

  return (
    <div className="group flex max-w-[75%] flex-col items-end self-end">
      {images && images.length > 0 && (
        <div className="mb-1 flex flex-wrap justify-end gap-1.5">
          {images.map((img, i) => {
            const src = imgSrc(img.file);
            return src ? (
              <button
                key={`${img.name}-${i}`}
                onClick={() => onImageOpen(src)}
                title={img.name}
                className="block h-28 w-28 overflow-hidden rounded-lg border border-border"
              >
                {/* Fixed square tile, cropped to fill — a very long screenshot
                    shows a neat preview instead of a thin sliver. */}
                <img
                  src={src}
                  alt={img.name}
                  className="h-full w-full cursor-zoom-in object-cover"
                />
              </button>
            ) : (
              <span
                key={`${img.name}-${i}`}
                className="flex items-center gap-1 rounded-md bg-raised px-2 py-1 text-[11px] text-ink-muted"
              >
                <IconImage size={12} className="text-ink-faint" />
                {img.name}
              </span>
            );
          })}
        </div>
      )}
      {text && (
        <div className="flex max-w-full flex-col items-end">
          <div
            ref={bodyRef}
            style={expanded ? undefined : { maxHeight: USER_MSG_COLLAPSE_PX }}
            className={`rounded-2xl rounded-br-md bg-raised px-3.5 py-2 text-base leading-[1.7] whitespace-pre-wrap [overflow-wrap:anywhere] ${
              expanded ? '' : 'overflow-hidden'
            }`}
          >
            {text}
          </div>
          {clamped && (
            <button
              onClick={() => setExpanded((v) => !v)}
              className="mt-1 text-[11px] text-ink-faint transition-colors hover:text-ink"
            >
              {expanded ? '收起' : '展开全部'}
            </button>
          )}
        </div>
      )}
      {/* In-flow so the gap is reserved and the hover reveal never overlaps
          the next message. */}
      <div className="mt-0.5 flex h-5 items-center gap-2 whitespace-nowrap opacity-0 transition-opacity group-hover:opacity-100">
        {createdAt && <span className="flex-none text-[11px] text-ink-faint">{hhmm(createdAt)}</span>}
        <CopyButton text={text} />
      </div>
    </div>
  );
}

function AssistantRow({ text, footer }: { text: string; footer?: TurnFooterData }) {
  return (
    <div className="max-w-[95%] self-start">
      <Markdown text={text} />
      {footer && <TurnFooter {...footer} />}
    </div>
  );
}

/** Quiet, always-visible end-of-turn marker: copy the turn's reply + its time. */
function TurnFooter({ text, createdAt }: TurnFooterData) {
  return (
    <div className="mt-1.5 -ml-1.5 flex items-center gap-2 whitespace-nowrap">
      <CopyButton text={text} />
      {createdAt && <span className="flex-none text-[11px] text-ink-faint">{hhmm(createdAt)}</span>}
    </div>
  );
}

/** A run of consecutive tool calls, folded into one quiet expandable line. */
function ToolGroup({ rows, footer }: { rows: RenderRow[]; footer?: TurnFooterData }) {
  const [open, setOpen] = useState(false);
  // Only a genuinely-running tool spins; 'done'/'incomplete' are terminal.
  const running = rows.some((r) => r.status === 'running');

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
      {/* When a turn was interrupted mid-tool, this group is the turn's last
          item, so the end-of-turn footer hangs off it instead of the assistant. */}
      {footer && <TurnFooter {...footer} />}
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
        ) : row.status === 'incomplete' ? (
          <span
            title="未完成：运行在结果返回前被中断"
            className="flex-none font-mono text-[13px] leading-none text-ink-faint"
          >
            –
          </span>
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
