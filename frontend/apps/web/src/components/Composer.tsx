import { useRef, useState } from 'react';
import { useStore } from '../store.js';
import { IconSend, IconStop } from './icons.js';

/**
 * Floating-card composer. Typing stays enabled during a run (drafting the next
 * instruction); only sending is gated. The send button doubles as the stop
 * button while a run is active.
 */
export function Composer() {
  const sendMessage = useStore((s) => s.sendMessage);
  const interrupt = useStore((s) => s.interrupt);
  const runActive = useStore((s) => s.runActive);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const taRef = useRef<HTMLTextAreaElement>(null);

  const canSend = !runActive && !sending && text.trim().length > 0;

  const autoGrow = () => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    ta.style.height = `${Math.min(ta.scrollHeight, 200)}px`;
  };

  const submit = async () => {
    const value = text.trim();
    if (!value || !canSend) return;
    setSending(true);
    setText('');
    if (taRef.current) taRef.current.style.height = 'auto';
    try {
      await sendMessage(value);
    } finally {
      setSending(false);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends; Shift+Enter inserts a newline.
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <div className="px-5 pb-4">
      <div className="mx-auto max-w-3xl">
        <div className="rounded-2xl border border-border bg-surface shadow-sm transition-colors focus-within:border-accent">
          <textarea
            ref={taRef}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              autoGrow();
            }}
            onKeyDown={onKeyDown}
            placeholder="描述要交给 agent 的任务…"
            rows={1}
            className="block max-h-50 w-full resize-none bg-transparent px-4 pt-3.5 pb-1 text-sm leading-relaxed outline-none placeholder:text-ink-faint"
          />
          <div className="flex items-center gap-2 px-3 pb-2.5">
            <span className="flex items-center gap-1.5 text-[11px] text-ink-faint">
              {runActive ? (
                <>
                  <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-accent" />
                  正在运行，可先写下一条指令
                </>
              ) : (
                'Enter 发送 · Shift+Enter 换行'
              )}
            </span>
            <div className="flex-1" />
            {runActive ? (
              <button
                onClick={() => void interrupt()}
                title="停止运行"
                className="flex h-8 w-8 flex-none items-center justify-center rounded-full border border-danger/50 text-danger transition-colors hover:bg-danger-soft"
              >
                <IconStop size={14} />
              </button>
            ) : (
              <button
                onClick={() => void submit()}
                disabled={!canSend}
                title="发送"
                className="flex h-8 w-8 flex-none items-center justify-center rounded-full bg-accent text-on-accent transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                <IconSend size={14} />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
