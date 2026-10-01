import { useCallback, useEffect, useRef, useState } from 'react';
import { call, usePrefs } from '../../api';
import { useT } from '../../i18n';
import { toast } from '../../ui';

/** Instant-save helper for per-user preferences: optimistic, with an Undo toast. */
export function usePrefSave() {
  const t = useT('settings');
  const { prefs, set } = usePrefs();
  const ref = useRef(prefs);
  ref.current = prefs;
  return useCallback(
    (key: string, value: unknown, name: string) => {
      const before = ref.current[key];
      set(key, value).then(
        () => toast.undo(t('saved', { name }), t('undo'), () => void set(key, before).catch(() => undefined)),
        (e) => toast.err(t('saveFailed', { name }), e instanceof Error ? e.message : undefined),
      );
    },
    [set, t],
  );
}

/** Reads a dotted key from either a flat map ({"login.show_ip": true}) or nested TOML-like JSON. */
export function pick(cfg: Record<string, any> | null, key: string): any {
  if (!cfg) return undefined;
  if (key in cfg) return cfg[key];
  let cur: any = cfg;
  for (const part of key.split('.')) {
    if (cur && typeof cur === 'object' && part in cur) cur = cur[part];
    else return undefined;
  }
  return cur;
}

export type CfgState = 'loading' | 'ready' | 'failed';

interface ConfigState {
  path: string;
  exists: boolean;
  values: Record<string, any>;
  keys: { key: string; type: string; restart?: boolean; values?: string[] }[];
  warnings: string[];
}

/**
 * Server configuration. config.get is readable by any user; config.set is an admin call, so the
 * global unlock dialog appears the first time something is changed.
 */
export function useServerConfig(enabled: boolean) {
  const t = useT('settings');
  const [info, setInfo] = useState<ConfigState | null>(null);
  const [state, setState] = useState<CfgState>('loading');
  const ref = useRef<ConfigState | null>(null);
  ref.current = info;

  const load = useCallback(async () => {
    setState('loading');
    try {
      const c = await call<ConfigState>('config.get', {}, { noUnlock: true });
      setInfo(c);
      setState('ready');
    } catch {
      setState('failed');
    }
  }, []);

  useEffect(() => {
    if (enabled) void load();
  }, [enabled, load]);

  const cfg = info?.values ?? null;
  const get = useCallback(<T,>(key: string, fallback: T): T => {
    const v = pick(cfg, key);
    return (v === undefined ? fallback : v) as T;
  }, [cfg]);

  const setKey = useCallback(
    async (key: string, value: unknown, name: string) => {
      const before = pick(ref.current?.values ?? null, key);
      const write = (v: unknown) => call<ConfigState>('config.set', { key, value: v });
      const patch = (v: unknown) => setInfo((c) => (c ? { ...c, values: { ...c.values, [key]: v } } : c));
      patch(value); // optimistic
      try {
        setInfo(await write(value));
        toast.undo(t('saved', { name }), t('undo'), () => {
          patch(before);
          write(before).then(setInfo, (e) => toast.err(t('saveFailed', { name }), e instanceof Error ? e.message : undefined));
        });
      } catch (e) {
        patch(before);
        toast.err(t('saveFailed', { name }), e instanceof Error ? e.message : undefined);
      }
    },
    [t],
  );

  return {
    cfg, state, get, set: setKey, reload: load,
    path: info?.path ?? '/etc/linuxadmin/linuxadmin.conf',
    warnings: info?.warnings ?? [],
    hasKey: (k: string) => !!info?.keys.some((x) => x.key === k),
    needsRestart: (k: string) => !!info?.keys.find((x) => x.key === k)?.restart,
  };
}
