import { useCallback, useEffect, useRef, useState } from 'react';
import { setUnlockRequester } from '../api/unlock';
import { ApiError, useSession } from '../api';
import { UnlockDialog } from '../ui';

/** Mount once: owns the global "Administrator rights needed" dialog used by call() on needs_admin. */
export function UnlockHost() {
  const { session, unlock } = useSession();
  const [open, setOpen] = useState(false);
  const pending = useRef<{ resolve(): void; reject(e: Error): void } | null>(null);

  useEffect(() => {
    setUnlockRequester(() => {
      setOpen(true);
      return new Promise<void>((resolve, reject) => {
        pending.current = { resolve, reject };
      });
    });
    return () => setUnlockRequester(null);
  }, []);

  const cancel = useCallback(() => {
    setOpen(false);
    pending.current?.reject(new ApiError('needs_admin', 'Administrator rights needed'));
    pending.current = null;
  }, []);

  return (
    <UnlockDialog
      open={open}
      user={session?.user}
      onCancel={cancel}
      onSubmit={async (pw) => {
        await unlock(pw);
        setOpen(false);
        pending.current?.resolve();
        pending.current = null;
      }}
    />
  );
}
