import { useEffect, useState } from 'react';
import { useStore } from './store.js';
import { getBridge } from './storage.js';
import { ConnectionSetup } from './components/ConnectionSetup.js';
import { MachineView } from './components/MachineView.js';
import { Overview } from './components/Overview.js';
import { Rail } from './components/Rail.js';
import { IconX } from './components/icons.js';

export function App() {
  const initialized = useStore((s) => s.initialized);
  const machines = useStore((s) => s.machines);
  const view = useStore((s) => s.view);
  const error = useStore((s) => s.error);
  const clearError = useStore((s) => s.clearError);
  const init = useStore((s) => s.init);

  const [adding, setAdding] = useState(false);

  // Load persisted machines on mount (localStorage on web, secure store on desktop).
  useEffect(() => {
    void init();
    // macOS desktop shell: enable drag regions + traffic-light inset (see styles.css).
    if (getBridge() && navigator.platform.toUpperCase().includes('MAC')) {
      document.documentElement.classList.add('desktop-mac');
    }
  }, [init]);

  if (!initialized) {
    return (
      <div className="startup-splash app-drag flex h-full items-center justify-center">
        <div className="text-center">
          <div className="startup-mark mx-auto flex h-11 w-11 items-center justify-center rounded-xl font-mono text-[11px] font-semibold tracking-[0.16em]">
            AM
          </div>
          <p className="mt-3 text-[11px] tracking-[0.16em] text-ink-faint">正在唤醒工作台</p>
        </div>
      </div>
    );
  }

  // First run: no machines yet — the add form is the whole page.
  if (machines.length === 0) {
    return <ConnectionSetup asModal={false} />;
  }

  return (
    <div className="flex h-full">
      <Rail onAddMachine={() => setAdding(true)} />
      {view === 'overview' ? <Overview /> : <MachineView />}

      {error && (
        <div className="fixed top-4 left-1/2 z-50 flex max-w-lg -translate-x-1/2 items-center gap-3 rounded-xl border border-danger/50 bg-danger-soft px-4 py-2.5 text-sm text-danger shadow-lg">
          <span className="min-w-0 flex-1">{error}</span>
          <button onClick={clearError} className="flex-none opacity-70 hover:opacity-100">
            <IconX size={14} />
          </button>
        </div>
      )}

      {adding && (
        <ConnectionSetup asModal onDone={() => setAdding(false)} onCancel={() => setAdding(false)} />
      )}
    </div>
  );
}
