import { useStore } from '../store.js';

/** Header control to switch between paired machines, add one, or remove the active one. */
export function MachineSwitcher({ onAdd }: { onAdd: () => void }) {
  const machines = useStore((s) => s.machines);
  const activeId = useStore((s) => s.activeMachineId);
  const selectMachine = useStore((s) => s.selectMachine);
  const removeMachine = useStore((s) => s.removeMachine);

  const active = machines.find((m) => m.id === activeId) ?? null;

  return (
    <div className="machine-switcher">
      <div className="machine-row">
        <select
          className="machine-select"
          value={activeId ?? ''}
          onChange={(e) => void selectMachine(e.target.value)}
        >
          {machines.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
        <button className="icon-btn" title="Add machine" onClick={onAdd}>
          +
        </button>
        {active && (
          <button
            className="icon-btn"
            title="Remove this machine"
            onClick={() => {
              if (window.confirm(`Remove machine "${active.name}"?`)) {
                void removeMachine(active.id);
              }
            }}
          >
            ×
          </button>
        )}
      </div>
      {active && (
        <div className="machine-url" title={active.baseUrl}>
          {active.baseUrl}
        </div>
      )}
    </div>
  );
}
