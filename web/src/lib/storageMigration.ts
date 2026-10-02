/**
 * Ervisio was called LinuxAdmin, and its web app kept this browser's settings (theme cache, language, cached
 * preferences, remembered accounts, terminal layout…) in localStorage keys starting with "la.". They now start
 * with "ervisio.". migrateStorage copies them over once, without overwriting a key that already has a value, then
 * removes the old ones. It runs before anything reads storage (main.tsx; public/theme-boot.js reads the theme
 * cache under either name), and never throws: storage can be unavailable (private mode, blocked site data).
 */
export const LEGACY_PREFIX = 'la.';
export const PREFIX = 'ervisio.';
export const MIGRATED_KEY = PREFIX + 'storageMigrated';

type KV = Pick<Storage, 'getItem' | 'setItem' | 'removeItem' | 'key' | 'length'>;

/** Returns how many keys were copied (0 when already done or nothing to do). */
export function migrateStorage(s: KV | null | undefined): number {
  if (!s) return 0;
  try {
    if (s.getItem(MIGRATED_KEY) === '1') return 0;
    const old: string[] = [];
    for (let i = 0; i < s.length; i++) {
      const k = s.key(i);
      if (k && k.startsWith(LEGACY_PREFIX)) old.push(k);
    }
    let copied = 0;
    for (const k of old) {
      const nk = PREFIX + k.slice(LEGACY_PREFIX.length);
      const v = s.getItem(k);
      if (v !== null && s.getItem(nk) === null) {
        s.setItem(nk, v);
        copied++;
      }
    }
    for (const k of old) s.removeItem(k);
    s.setItem(MIGRATED_KEY, '1');
    return copied;
  } catch {
    return 0;
  }
}
