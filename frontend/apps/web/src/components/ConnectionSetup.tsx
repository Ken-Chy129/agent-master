import { useState } from 'react';
import { defaultDaemonUrl } from '../lib/connectionUrl.js';
import { useEscape } from '../lib/useEscape.js';
import { getBridge } from '../storage.js';
import { useStore } from '../store.js';
import { IconCheck, IconChevronRight, IconTerminal, IconX } from './icons.js';

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
  const desktop = getBridge() !== null;
  const initialBaseUrl = defaultDaemonUrl(window.location);
  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState(initialBaseUrl);
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
      className={`connection-form relative w-full rounded-2xl border border-border bg-surface ${
        asModal ? 'max-w-md p-6 shadow-xl' : 'max-w-[29rem] p-6 shadow-xl sm:p-7'
      }`}
    >
      <div className={`mb-6 ${asModal ? 'pr-8' : ''}`}>
        <div className="flex items-center gap-3">
          <div className="connection-terminal flex h-10 w-10 flex-none items-center justify-center rounded-xl">
            <IconTerminal size={18} />
          </div>
          <h2 className="text-[18px] font-semibold tracking-[-0.02em]">
            {asModal ? '添加机器' : '连接第一台机器'}
          </h2>
        </div>
        <p className="mt-3 text-[12.5px] leading-5.5 text-ink-muted">
          在目标机器运行 <code className="connection-command">agent-master pair</code>，然后填入返回的地址与 token。
        </p>
      </div>
      {asModal && (
        <button
          type="button"
          onClick={onCancel}
          aria-label="关闭"
          className="connection-close absolute top-5 right-5 flex h-8 w-8 items-center justify-center rounded-lg text-ink-faint"
        >
          <IconX size={15} />
        </button>
      )}

      <Field label="名称（可选）">
        <input
          type="text"
          value={name}
          placeholder="例如：Mac Studio"
          onChange={(e) => setName(e.target.value)}
          autoFocus
          autoComplete="off"
          spellCheck={false}
          className="connection-input w-full rounded-xl px-3 py-2.5 text-sm outline-none placeholder:text-ink-faint"
        />
      </Field>
      <Field label="守护进程地址">
        <input
          type="text"
          value={baseUrl}
          placeholder={initialBaseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          className="connection-input w-full rounded-xl px-3 py-2.5 font-mono text-[13px] outline-none placeholder:text-ink-faint"
        />
      </Field>
      <Field label="Token">
        <input
          type="password"
          value={token}
          placeholder="Bearer token"
          onChange={(e) => setToken(e.target.value)}
          autoComplete="new-password"
          spellCheck={false}
          className="connection-input w-full rounded-xl px-3 py-2.5 font-mono text-[13px] outline-none placeholder:text-ink-faint"
        />
      </Field>

      {err && (
        <p role="alert" className="mb-3 rounded-lg bg-danger-soft px-3 py-2 text-xs text-danger">
          {err}
        </p>
      )}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={!canSubmit}
          aria-busy={busy}
          className="connection-submit flex flex-1 items-center justify-center gap-2 rounded-xl bg-accent py-2.5 text-sm font-medium text-on-accent transition-all disabled:opacity-40"
        >
          {busy ? '添加中…' : '添加机器'}
          {!busy && <IconChevronRight size={14} />}
        </button>
        {asModal && (
          <button
            type="button"
            onClick={onCancel}
            className="rounded-xl border border-border px-4 py-2.5 text-sm text-ink-muted transition-colors hover:border-border-strong hover:text-ink"
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
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4 backdrop-blur-[2px]"
        onClick={onCancel}
      >
        {form}
      </div>
    );
  }
  return (
    <div className="connection-stage app-drag h-full overflow-y-auto">
      <main className="grid min-h-full lg:grid-cols-[minmax(0,1.05fr)_minmax(430px,0.95fr)]">
        <section className="connection-intro order-2 flex min-h-[430px] items-center px-8 py-16 text-white lg:order-1 lg:px-12">
          <div className="ml-auto w-full max-w-[34rem] lg:pr-8">
            <div className="flex items-center gap-2 text-[10px] font-semibold tracking-[0.2em] text-white/55 uppercase">
              <span className="h-1.5 w-1.5 rounded-full bg-[#78a9ff] shadow-[0_0_10px_#78a9ff]" />
              Agent Master
            </div>
            <h1 className="connection-title mt-6 max-w-[30rem] text-[38px] leading-[1.08] font-semibold tracking-[-0.035em]">
              把每台开发机，收进一个安静的工作台。
            </h1>
            <p className="mt-5 max-w-[29rem] text-[14px] leading-7 text-white/62">
              查看跨机器任务、继续会话、处理待确认操作。
              {desktop ? '连接信息只保存在这台 Mac 的安全存储中。' : '连接信息只保存在当前浏览器中。'}
            </p>

            <div className="connection-rack mt-9" aria-hidden="true">
              <RackRow label="LOCAL" meta="127.0.0.1:8888" active />
              <RackRow label="BUILD" meta="ready" />
              <RackRow label="REMOTE" meta="standby" />
            </div>

            <div className="mt-8 grid gap-3 sm:grid-cols-3">
              <SetupStep number="01" text="启动守护进程" />
              <SetupStep number="02" text="运行 pair" />
              <SetupStep number="03" text="建立连接" />
            </div>
          </div>
        </section>

        <section className="order-1 flex items-center justify-center bg-canvas px-5 py-8 sm:px-6 lg:order-2 lg:px-10 lg:py-14">
          <div className="w-full max-w-[29rem]">
            {form}
            <p className="connection-storage-note mt-4 text-center text-[10.5px] leading-relaxed text-ink-faint">
              {desktop
                ? 'Token 通过系统安全存储持久化，不会写入普通浏览器存储。'
                : 'Token 保存在当前浏览器的本地存储中，请仅在可信设备上使用。'}
            </p>
          </div>
        </section>
      </main>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="mb-3.5 block">
      <span className="mb-1.5 block text-[11px] font-medium text-ink-muted">{label}</span>
      {children}
    </label>
  );
}

function RackRow({ label, meta, active = false }: { label: string; meta: string; active?: boolean }) {
  return (
    <div className="connection-rack-row flex items-center gap-3 px-4 py-3">
      <span className={`h-1.5 w-1.5 rounded-full ${active ? 'bg-[#78a9ff]' : 'bg-white/20'}`} />
      <span className="w-14 font-mono text-[10px] tracking-[0.12em] text-white/72">{label}</span>
      <span className="h-px flex-1 bg-white/8" />
      <span className="font-mono text-[10px] text-white/38">{meta}</span>
      {active && <IconCheck size={13} className="text-[#78a9ff]" />}
    </div>
  );
}

function SetupStep({ number, text }: { number: string; text: string }) {
  return (
    <div className="border-t border-white/10 pt-3">
      <span className="font-mono text-[9px] text-[#78a9ff]">{number}</span>
      <p className="mt-1 text-[11px] text-white/55">{text}</p>
    </div>
  );
}
