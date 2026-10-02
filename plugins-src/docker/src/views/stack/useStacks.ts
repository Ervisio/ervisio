import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { buildStacks, loadStackSources, type Stack, type StackSources } from '../../api/compose';
import { classify, type ErrorInfo } from '../../api/engine';
import { containers } from '../../api/resources';

/**
 * Stacks for the list and the stack page: folder + `docker compose ls` are read on mount, on reload() and when the
 * set of compose projects among the containers changes; container states stay live through the shared store.
 */
export function useStacks(): { stacks: Stack[]; sources: StackSources | undefined; loading: boolean; error: ErrorInfo | null; reload(): Promise<void> } {
  const { data: list, error: cerr } = containers.use();
  const [sources, setSources] = useState<StackSources>();
  const [error, setError] = useState<ErrorInfo | null>(null);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);

  const reload = useCallback(async () => {
    try {
      const s = await loadStackSources();
      if (alive.current) {
        setSources(s);
        setError(null);
      }
    } catch (e) {
      if (alive.current) setError(classify(e));
    }
    await containers.refresh();
  }, []);

  useEffect(() => { void reload(); }, [reload]);

  const key = useMemo(() => [...new Set((list ?? []).map((c) => c.Labels?.['com.docker.compose.project']).filter(Boolean))].sort().join(','), [list]);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) { first.current = false; return; }
    void reload();
  }, [key, reload]);

  const stacks = useMemo(() => (sources && list ? buildStacks(sources, list) : []), [sources, list]);
  return { stacks, sources, loading: !sources && !error && !cerr, error: error ?? (!sources ? cerr : null), reload };
}
