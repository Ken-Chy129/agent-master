import { useEffect } from 'react';

/**
 * Tiny anchored dropdown; closes on any outside click. The parent must be
 * `position: relative`; pass `className` to control the anchor corner
 * (defaults to below-right).
 */
export function Menu({
  children,
  onClose,
  className,
}: {
  children: React.ReactNode;
  onClose: () => void;
  className?: string;
}) {
  useEffect(() => {
    const close = () => onClose();
    window.addEventListener('click', close);
    return () => window.removeEventListener('click', close);
  }, [onClose]);
  return (
    <div
      className={`absolute z-30 w-max min-w-32 overflow-hidden rounded-lg border border-border bg-surface py-1 shadow-lg ${
        className ?? 'right-0 mt-1'
      }`}
      onClick={(e) => e.stopPropagation()}
    >
      {children}
    </div>
  );
}

export function MenuItem({
  icon,
  label,
  danger,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  danger?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-raised ${
        danger ? 'text-danger' : 'text-ink'
      }`}
      onClick={onClick}
    >
      {icon}
      {label}
    </button>
  );
}
