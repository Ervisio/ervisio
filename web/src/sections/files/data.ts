import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, stream } from '../../api';
import { fcall, onFilesChanged } from './fapi';
import type { FEntry, RawEntry } from './types';
import { basename, join, withPath } from './util';

export interface PaneData {
  entries: FEntry[];
  loading: boolean;
  error: ApiError | null;
  truncated: boolean;
  searching: boolean;
  reload(): void;
}

interface Args {
  loc: string;
  admin: boolean;
  showHidden: boolean;
  starred: string[];
  query: string;
  /** Called when the listing needed administrator rights and the user unlocked them. */
  onElevate(): void;
  /** Skip loading (the second pane while split view is off). */
  enabled: boolean;
}

interface TrashItem {
  id: string;
  originalPath: string;
  deletedAt: number;
  entry: RawEntry;
}

/** Loads what a pane shows: a folder, the Trash, recent or starred items, or search results. */
export function usePaneData(a: Args): PaneData {
  const [entries, setEntries] = useState<FEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [searching, setSearching] = useState(false);
  const [n, setN] = useState(0);
  const elev = useRef(a.onElevate);
  elev.current = a.onElevate;
  const starredKey = a.starred.join('\n');

  const reload = useCallback(() => setN((x) => x + 1), []);

  // reload when something changed in the folder this pane shows
  useEffect(
    () =>
      onFilesChanged((dir) => {
        if (!dir || dir === a.loc || a.loc.startsWith('trash:') || a.loc === 'recent:' || a.loc === 'starred:') reload();
      }),
    [a.loc, reload],
  );

  useEffect(() => {
    let live = true;
    const ctl = new AbortController();
    let handle: { close(): void } | null = null;
    const opts = { admin: a.admin, onElevate: () => elev.current(), signal: ctl.signal };
    const fail = (e: unknown) => {
      if (!live) return;
      if (e instanceof ApiError && e.code === 'cancelled') return;
      setError(e instanceof ApiError ? e : new ApiError('internal', e instanceof Error ? e.message : String(e)));
      setEntries([]);
      setLoading(false);
      setSearching(false);
    };
    if (!a.enabled) {
      setLoading(false);
      return () => {
        live = false;
      };
    }
    setLoading(true);
    setError(null);
    setTruncated(false);

    const q = a.query.trim();
    if (q && a.loc.startsWith('/')) {
      setSearching(true);
      setEntries([]);
      const timer = window.setTimeout(() => {
        const acc: FEntry[] = [];
        handle = stream<any>('files.search', { root: a.loc, query: q, maxResults: 500 }, {
          admin: a.admin,
          onData: (m) => {
            if (!live) return;
            if (m.entries) {
              for (const e of m.entries as RawEntry[]) acc.push(withPath(a.loc, e));
              setEntries([...acc]);
              setLoading(false);
            }
            if (m.done) {
              setTruncated(!!m.truncated);
              setSearching(false);
              setLoading(false);
            }
          },
          onEnd: () => {
            if (live) {
              setSearching(false);
              setLoading(false);
            }
          },
          onError: fail,
        });
      }, 300);
      return () => {
        live = false;
        window.clearTimeout(timer);
        handle?.close();
      };
    }
    setSearching(false);

    const run = async () => {
      if (a.loc === 'trash:') {
        const r = await fcall<{ items: TrashItem[] }>('files.trashList', {}, opts);
        return r.items.map<FEntry>((it) => ({ ...it.entry, path: it.entry.path ?? '', name: it.entry.name, trashId: it.id, origin: it.originalPath, deletedAt: it.deletedAt }));
      }
      if (a.loc === 'recent:') {
        const r = await fcall<{ recent: RawEntry[] }>('files.places', {}, opts);
        return r.recent.map((e) => withPath('/', e));
      }
      if (a.loc === 'starred:') {
        const out: FEntry[] = [];
        await Promise.all(
          starredKey
            .split('\n')
            .filter(Boolean)
            .map(async (p) => {
              try {
                out.push(withPath('/', await fcall<RawEntry>('files.stat', { path: p }, opts)));
              } catch {
                /* a starred item that no longer exists is skipped */
              }
            }),
        );
        return out.sort((x, y) => x.name.localeCompare(y.name));
      }
      const r = await fcall<{ path: string; entries: RawEntry[]; truncated?: boolean }>('files.list', { path: a.loc, showHidden: a.showHidden }, opts);
      if (live) setTruncated(!!r.truncated);
      return r.entries.map((e) => ({ ...e, path: join(r.path, e.name) }));
    };
    run().then(
      (list) => {
        if (!live) return;
        setEntries(list);
        setLoading(false);
      },
      fail,
    );
    return () => {
      live = false;
      ctl.abort();
    };
  }, [a.loc, a.admin, a.showHidden, a.query, a.enabled, n, starredKey]);

  return { entries, loading, error, truncated, searching, reload };
}

export const entryLabel = (e: FEntry) => e.name || basename(e.path);
