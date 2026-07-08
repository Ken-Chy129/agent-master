import { useEffect } from 'react';

/**
 * In-app confirmation dialog, replacing native window.confirm (which renders
 * as a jarring OS dialog in the Electron shell). Escape cancels; the confirm
 * button takes focus so Enter confirms.
 */
export function ConfirmDialog({
  title,
  description,
  confirmLabel = '确认',
  onConfirm,
  onCancel,
}: {
  title: string;
  description?: string;
  confirmLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onCancel]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={(e) => {
        e.stopPropagation();
        onCancel();
      }}
    >
      <div
        className="w-full max-w-sm rounded-2xl border border-border bg-surface p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-sm font-semibold">{title}</h2>
        {description && (
          <p className="mt-1.5 text-xs leading-relaxed text-ink-muted">{description}</p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            onClick={onCancel}
            className="rounded-lg border border-border px-3.5 py-1.5 text-xs text-ink-muted transition-colors hover:border-border-strong hover:text-ink"
          >
            取消
          </button>
          <button
            autoFocus
            onClick={onConfirm}
            className="rounded-lg bg-danger px-3.5 py-1.5 text-xs font-medium text-white transition-opacity hover:opacity-90"
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
