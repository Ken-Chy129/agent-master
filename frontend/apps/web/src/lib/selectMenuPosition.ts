export type SelectMenuPlacement = 'top' | 'bottom' | 'auto';

interface RectLike {
  top: number;
  bottom: number;
  left: number;
  right: number;
  width: number;
  height: number;
}

interface Size {
  width: number;
  height: number;
}

const VIEWPORT_MARGIN = 8;
const TRIGGER_GAP = 6;

/** Position a fixed menu next to its trigger while keeping it on-screen. */
export function positionSelectMenu(
  trigger: RectLike,
  menu: Size,
  viewport: Size,
  preferred: SelectMenuPlacement,
): { left: number; top: number } {
  const roomBelow = viewport.height - trigger.bottom - VIEWPORT_MARGIN;
  const roomAbove = trigger.top - VIEWPORT_MARGIN;
  const placement =
    preferred === 'auto'
      ? roomBelow >= menu.height + TRIGGER_GAP || roomBelow >= roomAbove
        ? 'bottom'
        : 'top'
      : preferred;

  const desiredTop =
    placement === 'top'
      ? trigger.top - menu.height - TRIGGER_GAP
      : trigger.bottom + TRIGGER_GAP;
  const maxTop = Math.max(VIEWPORT_MARGIN, viewport.height - menu.height - VIEWPORT_MARGIN);
  const top = Math.min(Math.max(VIEWPORT_MARGIN, desiredTop), maxTop);
  const maxLeft = Math.max(VIEWPORT_MARGIN, viewport.width - menu.width - VIEWPORT_MARGIN);
  const left = Math.min(Math.max(VIEWPORT_MARGIN, trigger.left), maxLeft);

  return { left, top };
}
