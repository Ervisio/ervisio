import { useEffect, useState } from 'react';
import { call, errorMessage } from '../../api';
import { useWindowActivity } from '../../lib/windowActivity';
import type { Remote, RemoteCapacity } from './types';

/** Two in-flight connections at most; no polling or network probes in minimized windows. */
export function useRemoteCapacity(connections: Remote[], enabled: boolean) {
  const active = useWindowActivity();
  const [capacity, setCapacity] = useState<Record<string, RemoteCapacity>>({});
  useEffect(() => {
    if (!enabled || !active) return;
    const controller = new AbortController();
    let index = 0;
    const worker = async () => {
      while (index < connections.length && !controller.signal.aborted) {
        const remote = connections[index++];
        let result: RemoteCapacity;
        try {
          result = await call<RemoteCapacity>(
            'remote.statfs',
            { conn: remote.id, path: remote.path || '/' },
            { noUnlock: true, signal: controller.signal },
          );
        } catch (error) {
          result = { supported: false, error: errorMessage(error) };
        }
        if (!controller.signal.aborted)
          setCapacity((current) => ({ ...current, [remote.id]: result }));
      }
    };
    void worker();
    void worker();
    return () => controller.abort();
  }, [connections, enabled, active]);
  return capacity;
}
