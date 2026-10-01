import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, call } from '../../api';
import { usePlugins } from '../../plugins';
import type { CatalogView, PluginInfo } from './types';

export interface PluginData {
  plugins: PluginInfo[];
  catalog: CatalogView | null;
  state: 'loading' | 'ready' | 'error';
  error: string;
  /** Reload the lists and ask the app's plugin loader to re-import enabled plugins. */
  refresh(reloadLoader?: boolean): Promise<void>;
}

export const errMsg = (e: unknown) => (e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e));

export function usePluginData(): PluginData {
  const loader = usePlugins();
  const [plugins, setPlugins] = useState<PluginInfo[]>([]);
  const [catalog, setCatalog] = useState<CatalogView | null>(null);
  const [state, setState] = useState<PluginData['state']>('loading');
  const [error, setError] = useState('');
  const reloadRef = useRef(loader.reload);
  reloadRef.current = loader.reload;

  const refresh = useCallback(async (reloadLoader = false) => {
    try {
      const l = await call<PluginInfo[] | null>('plugins.list');
      setPlugins(l ?? []);
      setState('ready');
      setError('');
    } catch (e) {
      setError(errMsg(e));
      setState('error');
    }
    try {
      setCatalog(await call<CatalogView>('plugins.catalog'));
    } catch {
      setCatalog((c) => c ?? { categories: [], plugins: [], warning: '' });
    }
    if (reloadLoader) reloadRef.current();
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return { plugins, catalog, state, error, refresh };
}
