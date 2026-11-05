import { useCallback, useEffect, useRef, useState } from 'react';
import { useWindowActivity } from '../../lib/windowActivity';
import { ApiError, stream, errorMessage } from '../../api';
import { fcall, onFilesChanged, type RunOpts } from './fapi';
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

const SEARCH_DEBOUNCE_MS = 300;
const SEARCH_MAX_RESULTS = 500;

/** Loads what a pane shows: a folder, the Trash, recent or starred items, or search results. */
export function usePaneData(a: Args): PaneData {
  const active = useWindowActivity();
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

  useEffect(
    () =>
      active
        ? onFilesChanged((dir) => {
            if (isAffectedBy(a.loc, dir)) reload();
          })
        : undefined,
    [a.loc, reload, active],
  );

  useEffect(() => {
    if (!active) return;
    let live = true;
    const isLive = () => live;
    const ctl = new AbortController();
    const opts: RunOpts = { admin: a.admin, onElevate: () => elev.current(), signal: ctl.signal };
    const fail = (e: unknown) => {
      if (!live) return;
      if (e instanceof ApiError && e.code === 'cancelled') return;
      setError(toApiError(e));
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
      let handle: { close(): void } | null = null;
      const view = { setEntries, setTruncated, setSearching, setLoading };
      const timer = window.setTimeout(() => {
        handle = streamSearch(a.loc, q, a.admin, isLive, view, fail);
      }, SEARCH_DEBOUNCE_MS);
      return () => {
        live = false;
        window.clearTimeout(timer);
        handle?.close();
      };
    }
    setSearching(false);

    const onTruncated = (v: boolean) => {
      if (live) setTruncated(v);
    };
    loadLocation(a.loc, starredKey, a.showHidden, opts, onTruncated).then((list) => {
      if (!live) return;
      setEntries(list);
      setLoading(false);
    }, fail);
    return () => {
      live = false;
      ctl.abort();
    };
  }, [a.loc, a.admin, a.showHidden, a.query, a.enabled, n, starredKey, active]);

  return { entries, loading, error, truncated, searching, reload };
}

/** Whether a change reported in `dir` (empty: anywhere) should reload the pane showing `loc`. */
function isAffectedBy(loc: string, dir: string | undefined): boolean {
  return !dir || dir === loc || loc.startsWith('trash:') || loc === 'recent:' || loc === 'starred:';
}

const toApiError = (e: unknown) =>
  e instanceof ApiError ? e : new ApiError('internal', errorMessage(e));

interface SearchMessage {
  entries?: RawEntry[];
  done?: boolean;
  truncated?: boolean;
}

interface SearchView {
  setEntries(entries: FEntry[]): void;
  setTruncated(truncated: boolean): void;
  setSearching(searching: boolean): void;
  setLoading(loading: boolean): void;
}

function streamSearch(
  root: string,
  query: string,
  admin: boolean,
  isLive: () => boolean,
  view: SearchView,
  onError: (e: unknown) => void,
) {
  const acc: FEntry[] = [];
  return stream<SearchMessage>(
    'files.search',
    { root, query, maxResults: SEARCH_MAX_RESULTS },
    {
      admin,
      onData: (m) => {
        if (!isLive()) return;
        if (m.entries) {
          for (const e of m.entries) acc.push(withPath(root, e));
          view.setEntries([...acc]);
          view.setLoading(false);
        }
        if (m.done) {
          view.setTruncated(!!m.truncated);
          view.setSearching(false);
          view.setLoading(false);
        }
      },
      onEnd: () => {
        if (isLive()) {
          view.setSearching(false);
          view.setLoading(false);
        }
      },
      onError,
    },
  );
}

async function loadLocation(
  loc: string,
  starredKey: string,
  showHidden: boolean,
  opts: RunOpts,
  onTruncated: (v: boolean) => void,
): Promise<FEntry[]> {
  if (loc === 'trash:') return loadTrash(opts);
  if (loc === 'recent:') return loadRecent(opts);
  if (loc === 'starred:') return loadStarred(starredKey.split('\n').filter(Boolean), opts);
  const r = await fcall<{ path: string; entries: RawEntry[]; truncated?: boolean }>(
    'files.list',
    { path: loc, showHidden },
    opts,
  );
  onTruncated(!!r.truncated);
  return r.entries.map((e) => ({ ...e, path: join(r.path, e.name) }));
}

async function loadTrash(opts: RunOpts): Promise<FEntry[]> {
  const r = await fcall<{ items: TrashItem[] }>('files.trashList', {}, opts);
  return r.items.map<FEntry>((it) => ({
    ...it.entry,
    path: it.entry.path ?? '',
    name: it.entry.name,
    trashId: it.id,
    origin: it.originalPath,
    deletedAt: it.deletedAt,
  }));
}

async function loadRecent(opts: RunOpts): Promise<FEntry[]> {
  const r = await fcall<{ recent: RawEntry[] }>('files.places', {}, opts);
  return r.recent.map((e) => withPath('/', e));
}

async function loadStarred(paths: string[], opts: RunOpts): Promise<FEntry[]> {
  const out: FEntry[] = [];
  await Promise.all(
    paths.map(async (p) => {
      try {
        out.push(withPath('/', await fcall<RawEntry>('files.stat', { path: p }, opts)));
      } catch {
        /* a starred item that no longer exists is skipped */
      }
    }),
  );
  return out.sort((x, y) => x.name.localeCompare(y.name));
}

export const entryLabel = (e: FEntry) => e.name || basename(e.path);
