import { useEffect, useState } from 'react';
import { call, errorMessage } from '../../api';
import { useRefreshInterval } from '../../lib/refresh';
import { useWindowActivity } from '../../lib/windowActivity';
import { useAccountScope } from '../../lib/accountScope';
import type { TaskSnapshot } from './model';

/** Sequential reads; minimized/hidden windows abort the current read and remove their timer. */
export function useTaskSnapshot() {
  const active = useWindowActivity();
  const interval = useRefreshInterval();
  const scope = useAccountScope();
  const [state, setState] = useState<{
    scope: string | null;
    snapshot: TaskSnapshot | null;
    error: string;
    loading: boolean;
  }>({ scope: null, snapshot: null, error: '', loading: true });
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!active || !scope) return;
    let stopped = false;
    let timer: number | undefined;
    let controller: AbortController;
    const load = async () => {
      controller = new AbortController();
      try {
        const next = await call<TaskSnapshot>(
          'overview.taskManager',
          {},
          { noUnlock: true, signal: controller.signal },
        );
        if (!next || !Array.isArray(next.processes) || !next.capabilities?.gpu)
          throw new Error('Invalid process telemetry response');
        if (!stopped)
          setState({
            scope,
            snapshot: { ...next, gpus: next.gpus ?? [] },
            error: '',
            loading: false,
          });
      } catch (failure) {
        if (!stopped)
          setState((old) => ({
            scope,
            snapshot: old.scope === scope ? old.snapshot : null,
            error: errorMessage(failure),
            loading: false,
          }));
      } finally {
        if (!stopped) timer = window.setTimeout(load, interval);
      }
    };
    void load();
    return () => {
      stopped = true;
      controller?.abort();
      window.clearTimeout(timer);
    };
  }, [active, interval, revision, scope]);
  const visible = state.scope === scope ? state : { snapshot: null, error: '', loading: true };
  return { ...visible, refresh: () => setRevision((v) => v + 1) };
}
