import { useState } from 'react';
import { useStore } from '../store.js';
import { IconSend } from './icons.js';

export function Composer() {
  const sendMessage = useStore((s) => s.sendMessage);
  const runActive = useStore((s) => s.runActive);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);

  const disabled = runActive || sending;

  const submit = async () => {
    const value = text.trim();
    if (!value || disabled) return;
    setSending(true);
    setText('');
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
    <div className="border-t border-border bg-surface px-4 py-3">
      <div className="mx-auto flex max-w-2xl items-end gap-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={runActive ? '运行中…（可点上方「中断」）' : '发消息，Enter 发送，Shift+Enter 换行'}
          rows={1}
          disabled={disabled}
          className="max-h-40 min-h-10 flex-1 resize-none rounded-xl border border-border bg-canvas px-3.5 py-2.5 text-sm outline-none placeholder:text-ink-faint focus:border-accent disabled:opacity-60"
        />
        <button
          onClick={() => void submit()}
          disabled={disabled || !text.trim()}
          title="发送"
          className="flex h-10 w-10 flex-none items-center justify-center rounded-xl bg-accent text-on-accent transition-opacity hover:opacity-90 disabled:opacity-40"
        >
          <IconSend size={16} />
        </button>
      </div>
    </div>
  );
}
