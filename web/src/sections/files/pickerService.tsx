import { useLayoutEffect, useSyncExternalStore } from 'react';
import { ApiError, useSession } from '../../api';
import { useAccountScope } from '../../lib/accountScope';
import { FilePicker } from './FilePicker';

export interface PickFileOptions {
  mode: 'file' | 'folder' | 'save';
  title?: string;
  initialDirectory?: string;
  initialName?: string;
  localOnly?: boolean;
}
type PickResult = { path: string } | null;
interface Pending {
  id: number;
  options: PickFileOptions;
  plugin: string;
  owner: string;
  resolve(value: PickResult): void;
  dispose(): void;
}
let pending: Pending | null = null;
let sequence = 0;
let owner = '';
const listeners = new Set<() => void>();
const notify = () => listeners.forEach((listener) => listener());
function finish(value: PickResult) {
  const request = pending;
  pending = null;
  request?.dispose();
  request?.resolve(value);
  notify();
}

/** A chooser result identifies a user-selected path; it grants no filesystem capability. */
export function requestFilePicker(
  plugin: string,
  options: PickFileOptions,
  signal?: AbortSignal,
): Promise<PickResult> {
  if (!owner)
    return Promise.reject(new ApiError('unavailable', 'The file chooser is not available.'));
  if (pending)
    return Promise.reject(new ApiError('conflict', 'Finish the open file chooser first.'));
  if (signal?.aborted) return Promise.resolve(null);
  return new Promise((resolve) => {
    const cancel = () => {
      if (pending === request) finish(null);
    };
    const request: Pending = {
      id: ++sequence,
      options,
      plugin,
      owner,
      resolve,
      dispose: () => signal?.removeEventListener('abort', cancel),
    };
    pending = request;
    signal?.addEventListener('abort', cancel, { once: true });
    notify();
  });
}

export function FilePickerHost() {
  const scope = useAccountScope();
  const { session } = useSession();
  const request = useSyncExternalStore(
    (callback) => {
      listeners.add(callback);
      return () => {
        listeners.delete(callback);
      };
    },
    () => pending,
  );
  useLayoutEffect(() => {
    owner = scope ?? '';
    return () => {
      if (pending?.owner === scope) finish(null);
      if (owner === scope) owner = '';
    };
  }, [scope]);
  if (!request || request.owner !== scope) return null;
  const options = request.options;
  return (
    <FilePicker
      key={request.id}
      mode={options.mode}
      title={options.title ? `${request.plugin} · ${options.title}` : request.plugin}
      initialDirectory={options.initialDirectory || session?.home || '/'}
      initialName={options.initialName}
      localOnly={options.localOnly}
      onCancel={() => finish(null)}
      onChoose={(path) => finish({ path })}
    />
  );
}
