import type { MachineProfile } from '@agent-master/core';

export type DropPlacement = 'before' | 'after';

/** Return a new machine list with one item moved around a drop target. */
export function reorderMachines(
  machines: MachineProfile[],
  movedId: string,
  targetId: string,
  placement: DropPlacement,
): MachineProfile[] {
  if (movedId === targetId) return machines;

  const movedIndex = machines.findIndex((machine) => machine.id === movedId);
  const targetIndex = machines.findIndex((machine) => machine.id === targetId);
  if (movedIndex < 0 || targetIndex < 0) return machines;

  const next = [...machines];
  const [moved] = next.splice(movedIndex, 1);
  if (!moved) return machines;

  const adjustedTargetIndex = next.findIndex((machine) => machine.id === targetId);
  const insertIndex = placement === 'after' ? adjustedTargetIndex + 1 : adjustedTargetIndex;
  next.splice(insertIndex, 0, moved);

  if (next.every((machine, index) => machine === machines[index])) return machines;
  return next;
}
