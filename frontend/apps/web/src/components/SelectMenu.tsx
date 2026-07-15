import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { SelectMenuPlacement } from '../lib/selectMenuPosition.js';
import { positionSelectMenu } from '../lib/selectMenuPosition.js';
import { IconCheck, IconChevronRight } from './icons.js';

export interface SelectMenuOption {
  value: string;
  label: string;
  description?: string;
  disabled?: boolean;
}

const useBrowserLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect;

export function SelectMenu({
  value,
  options,
  onChange,
  ariaLabel,
  displayLabel,
  placement = 'auto',
  disabled = false,
  title,
  buttonClassName = '',
}: {
  value: string;
  options: SelectMenuOption[];
  onChange: (value: string) => void;
  ariaLabel: string;
  displayLabel?: string;
  placement?: SelectMenuPlacement;
  disabled?: boolean;
  title?: string;
  buttonClassName?: string;
}) {
  const listboxId = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [position, setPosition] = useState<{ left: number; top: number; minWidth: number } | null>(
    null,
  );

  const selectedIndex = Math.max(
    0,
    options.findIndex((option) => option.value === value),
  );
  const selected = options[selectedIndex];

  const close = (restoreFocus = false) => {
    setOpen(false);
    setPosition(null);
    if (restoreFocus) requestAnimationFrame(() => triggerRef.current?.focus());
  };

  const show = (index = selectedIndex) => {
    if (disabled || options.length === 0) return;
    setActiveIndex(index);
    setOpen(true);
  };

  const moveActive = (direction: -1 | 1) => {
    if (options.length === 0) return;
    let next = activeIndex;
    for (let count = 0; count < options.length; count += 1) {
      next = (next + direction + options.length) % options.length;
      if (!options[next]?.disabled) break;
    }
    setActiveIndex(next);
  };

  useBrowserLayoutEffect(() => {
    if (!open) return;
    const update = () => {
      const trigger = triggerRef.current;
      const menu = menuRef.current;
      if (!trigger || !menu) return;
      const rect = trigger.getBoundingClientRect();
      const next = positionSelectMenu(
        rect,
        { width: Math.max(menu.offsetWidth, rect.width), height: menu.offsetHeight },
        { width: window.innerWidth, height: window.innerHeight },
        placement,
      );
      setPosition({ ...next, minWidth: rect.width });
    };
    update();
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, true);
    return () => {
      window.removeEventListener('resize', update);
      window.removeEventListener('scroll', update, true);
    };
  }, [open, placement, options.length]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      const node = event.target as Node;
      if (triggerRef.current?.contains(node) || menuRef.current?.contains(node)) return;
      close();
    };
    document.addEventListener('pointerdown', onPointerDown);
    requestAnimationFrame(() => menuRef.current?.focus());
    return () => document.removeEventListener('pointerdown', onPointerDown);
  }, [open]);

  const choose = (index: number) => {
    const option = options[index];
    if (!option || option.disabled) return;
    onChange(option.value);
    close(true);
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        disabled={disabled}
        title={title}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        onClick={() => (open ? close() : show())}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault();
            show(event.key === 'ArrowUp' ? options.length - 1 : selectedIndex);
          }
        }}
        className={`select-trigger flex min-w-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-left text-[11px] outline-none ${buttonClassName}`}
      >
        <span className="min-w-0 flex-1 truncate">{displayLabel ?? selected?.label ?? ariaLabel}</span>
        <IconChevronRight
          size={11}
          className={`flex-none transition-transform ${open ? '-rotate-90' : 'rotate-90'}`}
        />
      </button>

      {open &&
        typeof document !== 'undefined' &&
        createPortal(
          <div
            ref={menuRef}
            id={listboxId}
            role="listbox"
            tabIndex={-1}
            aria-label={ariaLabel}
            aria-activedescendant={`${listboxId}-${activeIndex}`}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                event.preventDefault();
                moveActive(event.key === 'ArrowDown' ? 1 : -1);
              } else if (event.key === 'Home') {
                event.preventDefault();
                setActiveIndex(0);
              } else if (event.key === 'End') {
                event.preventDefault();
                setActiveIndex(options.length - 1);
              } else if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                choose(activeIndex);
              } else if (event.key === 'Escape') {
                event.preventDefault();
                close(true);
              } else if (event.key === 'Tab') {
                close();
              }
            }}
            className="select-popover fixed z-[70] w-max max-w-[calc(100vw-1rem)] overflow-hidden rounded-xl border border-border bg-surface p-1.5 shadow-xl outline-none"
            style={{
              left: position?.left ?? 0,
              top: position?.top ?? 0,
              minWidth: position?.minWidth,
              visibility: position ? 'visible' : 'hidden',
            }}
          >
            {options.map((option, index) => {
              const selectedOption = option.value === value;
              const active = index === activeIndex;
              return (
                <button
                  key={option.value || 'default'}
                  id={`${listboxId}-${index}`}
                  type="button"
                  role="option"
                  aria-selected={selectedOption}
                  disabled={option.disabled}
                  onPointerMove={() => setActiveIndex(index)}
                  onClick={() => choose(index)}
                  className={`select-option flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] ${
                    active ? 'select-option-active' : ''
                  }`}
                >
                  <span className="flex h-4 w-4 flex-none items-center justify-center text-accent">
                    {selectedOption && <IconCheck size={13} />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium text-ink">{option.label}</span>
                    {option.description && (
                      <span className="mt-0.5 block truncate text-[10px] text-ink-faint">
                        {option.description}
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>,
          document.body,
        )}
    </>
  );
}
