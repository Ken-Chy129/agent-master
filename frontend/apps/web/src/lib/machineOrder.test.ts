import type { MachineProfile } from '@agent-master/core';
import { describe, expect, it } from 'vitest';
import { reorderMachines } from './machineOrder.js';

const machines: MachineProfile[] = [
  { id: 'sg1', name: 'SG1', baseUrl: 'http://sg1', token: 'a' },
  { id: 'mac', name: 'MAC', baseUrl: 'http://mac', token: 'b' },
  { id: 'ser', name: 'SER', baseUrl: 'http://ser', token: 'c' },
];

describe('reorderMachines', () => {
  it('moves a machine before the selected drop target', () => {
    const reordered = reorderMachines(machines, 'ser', 'sg1', 'before');

    expect(reordered.map((machine) => machine.id)).toEqual(['ser', 'sg1', 'mac']);
  });

  it('moves a machine after the selected drop target', () => {
    const reordered = reorderMachines(machines, 'sg1', 'mac', 'after');

    expect(reordered.map((machine) => machine.id)).toEqual(['mac', 'sg1', 'ser']);
  });

  it('leaves the order unchanged for an invalid drag', () => {
    expect(reorderMachines(machines, 'missing', 'mac', 'before')).toBe(machines);
    expect(reorderMachines(machines, 'mac', 'mac', 'after')).toBe(machines);
  });

  it('keeps the original array when the requested placement already matches', () => {
    expect(reorderMachines(machines, 'sg1', 'mac', 'before')).toBe(machines);
    expect(reorderMachines(machines, 'mac', 'sg1', 'after')).toBe(machines);
  });
});
