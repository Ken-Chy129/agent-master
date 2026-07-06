import { useState } from 'react';
import { useStore } from '../store.js';

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
    <div className="composer">
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={runActive ? 'Run in progress…' : 'Message  (Enter to send, Shift+Enter for newline)'}
        rows={1}
        disabled={disabled}
      />
      <div className="actions">
        <button className="primary" onClick={() => void submit()} disabled={disabled || !text.trim()}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>
    </div>
  );
}
