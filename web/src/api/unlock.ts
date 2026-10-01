import { ApiError } from './types';

type Requester = (reason?: string) => Promise<void>;
let requester: Requester | null = null;
let pending: Promise<void> | null = null;

/** The shell registers the dialog opener here. */
export function setUnlockRequester(fn: Requester | null) {
  requester = fn;
}

/** Resolves once the user unlocked; rejects with ApiError('needs_admin') if cancelled. Concurrent callers share one dialog. */
export function requestUnlock(reason?: string): Promise<void> {
  if (!requester) return Promise.reject(new ApiError('needs_admin', 'Administrator rights needed'));
  if (!pending) {
    pending = requester(reason).finally(() => {
      pending = null;
    });
  }
  return pending;
}
