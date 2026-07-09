import { useEffect, useRef } from 'react';

/**
 * Calls `onEscape` when the Escape key is pressed while `active` is true.
 * The callback is held in a ref so passing an inline arrow doesn't re-bind the
 * listener on every render.
 */
export function useEscape(onEscape: () => void, active = true): void {
  const ref = useRef(onEscape);
  ref.current = onEscape;
  useEffect(() => {
    if (!active) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      ref.current();
      // Esc is a keyboard action, so focus would otherwise linger on the
      // trigger with a :focus-visible ring after the overlay closes. Drop it.
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [active]);
}
