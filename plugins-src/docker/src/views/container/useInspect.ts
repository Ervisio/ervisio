import { useEffect, useRef } from 'react';
import { docker } from '../../api/engine';
import { useAsync, useDockerEvents } from '../../api/hooks';
import type { ContainerInspect } from '../../api/types';

/** Loads /containers/{id}/json and reloads it (quietly) when the Engine reports a change for this container. */
export function useInspect(id: string) {
  const r = useAsync(() => docker.get<ContainerInspect>(`/containers/${encodeURIComponent(id)}/json`), [id]);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const reload = r.reload;
  const full = r.data?.Id;
  useDockerEvents((ev) => {
    if (ev.Type !== 'container' || ev.Action.startsWith('exec_') || ev.Action === 'attach') return;
    const evId = ev.Actor?.ID ?? '';
    if (!(evId === full || evId.startsWith(id) || id.startsWith(evId))) return;
    clearTimeout(timer.current);
    timer.current = setTimeout(reload, 300);
  });
  useEffect(() => () => clearTimeout(timer.current), []);
  return r;
}
