import { useState } from 'react';
import { useEscape } from '../lib/useEscape.js';
import { useStore } from '../store.js';

const DEFAULT_BASE_URL = 'http://localhost:8888';

/**
 * Add-machine form. Rendered as a centered card on first run (no machines yet)
 * and as a modal overlay afterwards.
 */
export function ConnectionSetup({
  asModal,
  onDone,
  onCancel,
}: {
  asModal: boolean;
  onDone?: () => void;
  onCancel?: () => void;
}) {
  const addMachine = useStore((s) => s.addMachine);
  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState(DEFAULT_BASE_URL);
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Esc closes the add-machine modal (only when it's dismissible).
  useEscape(() => onCancel?.(), asModal);

  const canSubmit = baseUrl.trim().length > 0 && token.trim().length > 0 && !busy;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    setErr(null);
    try {
      await addMachine({
        name: name.trim() || undefined,
        baseUrl: baseUrl.trim().replace(/\/+$/, ''),
        token: token.trim(),
      });
      onDone?.();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const form = (
    <form
      onSubmit={submit}
      onClick={(e) => e.stopPropagation()}
      className="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl"
    >
      <h1 className="text-base font-semibold">添加机器</h1>
      <p className="mt-1 mb-4 text-xs leading-relaxed text-ink-muted">
        填入目标机器上 agent-master 守护进程的地址和 token。在那台机器上运行{' '}
        <code className="rounded bg-raised px-1 py-0.5 font-mono text-[11px]">
          agent-master pair
        </code>{' '}
        即可获取。
      </p>

      <Field label="名称（可选）">
        <input
          type="text"
          value={name}
          placeholder="例如：dev-01"
          onChange={(e) => setName(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          className="w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm outline-none placeholder:text-ink-faint focus:border-accent"
        />
      </Field>
      <Field label="守护进程地址">
        <input
          type="text"
          value={baseUrl}
          placeholder={DEFAULT_BASE_URL}
          onChange={(e) => setBaseUrl(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          className="w-full rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-sm outline-none placeholder:text-ink-faint focus:border-accent"
        />
      </Field>
      <Field label="Token">
        <input
          type="password"
          value={token}
          placeholder="Bearer token"
          onChange={(e) => setToken(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          className="w-full rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-sm outline-none placeholder:text-ink-faint focus:border-accent"
        />
      </Field>

      {err && <p className="mb-3 text-xs text-danger">{err}</p>}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={!canSubmit}
          className="flex-1 rounded-lg bg-accent py-2 text-sm font-medium text-on-accent transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {busy ? '添加中…' : '添加机器'}
        </button>
        {asModal && (
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-border px-4 py-2 text-sm text-ink-muted transition-colors hover:border-border-strong hover:text-ink"
          >
            取消
          </button>
        )}
      </div>
    </form>
  );

  if (asModal) {
    return (
      <div
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
        onClick={onCancel}
      >
        {form}
      </div>
    );
  }
  // First-run full page: also serves as the window drag region on macOS
  // (the form itself is excluded from dragging via .app-drag form).
  return <div className="app-drag flex h-full items-center justify-center p-4">{form}</div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="mb-3 block">
      <span className="mb-1 block text-xs text-ink-muted">{label}</span>
      {children}
    </label>
  );
}
