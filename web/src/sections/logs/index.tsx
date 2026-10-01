import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ApiError, call, stream, usePrefs, useSession, type StreamHandle } from '../../api';
import { useT } from '../../i18n';
import { usePaletteActions } from '../index';
import { Badge, Button, Page, Sheet, toast, useIsMobile, useMediaQuery } from '../../ui';
import { Details } from './Details';
import { FilterBar } from './FilterBar';
import { Histogram } from './Histogram';
import { Sources } from './Sources';
import { Stream } from './Stream';
import { bounds, normalizeWatchers, parseFilter, spanMs, writeFilter, type Filter } from './helpers';
import { LEVELS, type Entry, type HistogramResult, type Level, type QueryResult, type SourcesResult, type Watcher } from './types';
import './logs.css';

const PAGE = 400;
const MAX_ROWS = 20000;
let seq = 0;

function toEntry(e: Omit<Entry, 'key'>): Entry {
  return { ...e, key: e.file && !e.line ? `${e.cursor}#${++seq}` : e.cursor };
}

const message = (e: unknown) => (e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e));

export default function LogsPage() {
  const t = useT('logs');
  const nav = useNavigate();
  const mobile = useIsMobile();
  const wide = useMediaQuery('(min-width: 1100px)');
  const inlineDetails = useMediaQuery('(min-width: 1360px)');
  const [params, setParams] = useSearchParams();
  const { prefs, set } = usePrefs();
  const { session, isUnlocked } = useSession();

  const filter = useMemo(() => parseFilter(params), [params]);
  const setFilter = useCallback((patch: Partial<Filter>) => setParams((prev) => writeFilter(prev, patch), { replace: true }), [setParams]);

  const watchers = useMemo(() => normalizeWatchers(prefs['logs.watchers']), [prefs]);
  const watchersKey = JSON.stringify(watchers);

  // ---- admin: ask as the user first, escalate when the bridge says needs_admin ----
  const adminRef = useRef(false);
  const [admin, setAdmin] = useState(false);
  useEffect(() => {
    if (!isUnlocked && !session?.isRoot && adminRef.current) {
      adminRef.current = false;
      setAdmin(false);
    }
  }, [isUnlocked, session?.isRoot]);
  const lcall = useCallback(async <T,>(method: string, p: unknown, signal?: AbortSignal): Promise<T> => {
    if (adminRef.current) return call<T>(method, p, { admin: true, signal });
    try {
      return await call<T>(method, p, { signal, noUnlock: true });
    } catch (e) {
      if (!(e instanceof ApiError) || e.code !== 'needs_admin') throw e;
      const r = await call<T>(method, p, { admin: true, signal });
      adminRef.current = true;
      setAdmin(true);
      return r;
    }
  }, []);
  const unlockAndReload = useCallback(() => {
    void call('logs.sources', { watchers }, { admin: true })
      .then(() => {
        adminRef.current = true;
        setAdmin(true);
        setTick((n) => n + 1);
      })
      .catch(() => undefined);
  }, [watchers]);

  // ---- sources ----
  const [sources, setSources] = useState<SourcesResult | null>(null);
  const [srcError, setSrcError] = useState<string>();
  const loadSources = useCallback(
    (signal?: AbortSignal) => {
      setSrcError(undefined);
      lcall<SourcesResult>('logs.sources', { watchers }, signal)
        .then((r) => !signal?.aborted && setSources(r))
        .catch((e) => !signal?.aborted && setSrcError(message(e)));
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [lcall, watchersKey],
  );
  useEffect(() => {
    const ac = new AbortController();
    loadSources(ac.signal);
    const id = window.setInterval(() => loadSources(ac.signal), 60_000);
    return () => {
      ac.abort();
      window.clearInterval(id);
    };
  }, [loadSources, admin]);

  // ---- query ----
  const [tick, setTick] = useState(0);
  const anchor = useMemo(() => Date.now(), [filter.range, filter.from, filter.to, tick]); // eslint-disable-line react-hooks/exhaustive-deps
  const b = useMemo(() => bounds(filter, anchor), [filter, anchor]);
  const qkey = JSON.stringify([filter.sources, filter.levels, filter.q, filter.range, filter.from, filter.to, tick, watchersKey]);

  const [rows, setRows] = useState<Entry[]>([]);
  const rowsRef = useRef<Entry[]>([]);
  rowsRef.current = rows;
  const [next, setNext] = useState<string | undefined>();
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [moreLoading, setMoreLoading] = useState(false);
  const [error, setError] = useState<string>();
  const [skipped, setSkipped] = useState(0);
  const [loadedKey, setLoadedKey] = useState('');
  const [sel, setSel] = useState<string | null>(null);
  const gen = useRef(0);
  const moreAc = useRef<AbortController | null>(null);

  const baseParams = useCallback(
    () => ({ sources: filter.sources, since: b.since, until: b.until, levels: filter.levels, text: filter.q, watchers }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [qkey],
  );

  useEffect(() => {
    const ac = new AbortController();
    const id = ++gen.current;
    moreAc.current?.abort();
    setLoading(true);
    setError(undefined);
    setRows([]);
    setNext(undefined);
    setHasMore(false);
    setSel(null);
    setMoreLoading(false);
    lcall<QueryResult>('logs.query', { ...baseParams(), limit: PAGE }, ac.signal)
      .then((r) => {
        if (id !== gen.current) return;
        setRows(r.entries.map(toEntry));
        setNext(r.next);
        setHasMore(r.hasMore);
        setSkipped((r.skipped ?? []).filter((s) => s.code === 'needs_admin').length);
        setLoading(false);
        setLoadedKey(qkey);
      })
      .catch((e) => {
        if (id !== gen.current || ac.signal.aborted) return;
        setError(message(e));
        setLoading(false);
      });
    return () => ac.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qkey]);

  const loadMore = useCallback(() => {
    if (!next || moreLoading) return;
    const id = gen.current;
    const ac = new AbortController();
    moreAc.current = ac;
    setMoreLoading(true);
    setError(undefined);
    lcall<QueryResult>('logs.query', { ...baseParams(), limit: PAGE, cursor: next }, ac.signal)
      .then((r) => {
        if (id !== gen.current) return;
        setRows((prev) => prev.concat(r.entries.map(toEntry)));
        setNext(r.next);
        setHasMore(r.hasMore);
      })
      .catch((e) => {
        if (id !== gen.current || ac.signal.aborted) return;
        setError(message(e));
      })
      .finally(() => id === gen.current && setMoreLoading(false));
  }, [next, moreLoading, lcall, baseParams]);

  // ---- histogram ----
  const [hist, setHist] = useState<HistogramResult | null>(null);
  const [histLoading, setHistLoading] = useState(false);
  const [hTick, setHTick] = useState(0);
  useEffect(() => {
    if (!filter.live) return;
    const id = window.setInterval(() => setHTick((n) => n + 1), 20_000);
    return () => window.clearInterval(id);
  }, [filter.live]);
  useEffect(() => {
    const ac = new AbortController();
    const now = filter.live ? Date.now() : anchor;
    const hb = bounds(filter, now);
    setHistLoading(true);
    lcall<HistogramResult>('logs.histogram', { sources: filter.sources, since: hb.since, until: hb.until ?? now, text: filter.q, buckets: 60, watchers }, ac.signal)
      .then((r) => {
        if (ac.signal.aborted) return;
        setHist(r);
        setHistLoading(false);
      })
      .catch(() => !ac.signal.aborted && setHistLoading(false));
    return () => ac.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter.sources, filter.q, filter.range, filter.from, filter.to, filter.live, tick, hTick, watchersKey]);

  const counts = useMemo<Record<Level, number> | null>(() => {
    if (!hist) return null;
    const c: Record<Level, number> = { err: 0, warn: 0, info: 0, debug: 0 };
    for (const x of hist.buckets) {
      c.err += x.err;
      c.warn += x.warn;
      c.info += x.info;
      c.debug += x.debug;
    }
    return c;
  }, [hist]);

  // ---- live ----
  useEffect(() => {
    if (!filter.live || loadedKey !== qkey) return;
    let h: StreamHandle | undefined;
    let closed = false;
    const ingest = (batch: Omit<Entry, 'key'>[]) => {
      if (!Array.isArray(batch) || !batch.length) return;
      const recent = new Set(rowsRef.current.slice(0, 600).map((r) => r.cursor));
      const fresh = batch.filter((e) => e.file || !recent.has(e.cursor)).map(toEntry).reverse();
      if (!fresh.length) return;
      setRows((prev) => {
        const nx = fresh.concat(prev);
        return nx.length > MAX_ROWS ? nx.slice(0, MAX_ROWS) : nx;
      });
    };
    const open = (adm: boolean) => {
      h = stream<Omit<Entry, 'key'>[]>(
        'logs.follow',
        { sources: filter.sources, levels: filter.levels, text: filter.q, watchers },
        {
          admin: adm,
          reopen: true,
          onData: ingest,
          onError: (e) => {
            if (closed) return;
            if (e.code === 'needs_admin' && !adm) {
              adminRef.current = true;
              setAdmin(true);
              open(true);
              return;
            }
            toast.err(t('live.ended', { message: e.message }));
            setFilter({ live: false });
          },
        },
      );
    };
    open(adminRef.current);
    return () => {
      closed = true;
      h?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter.live, loadedKey === qkey, qkey]);

  // ---- selection and navigation ----
  const selected = useMemo(() => (sel ? rows.find((r) => r.key === sel) ?? null : null), [rows, sel]);
  const onlySource = filter.sources.length === 1 ? filter.sources[0] : '';
  const contextSource = selected ? (onlySource.startsWith('unit:') || onlySource === 'kernel' ? onlySource : selected.srcId) : 'journal';

  const addWatcher = (w: Watcher) => {
    void set('logs.watchers', [...watchers, w]);
    setFilter({ sources: [`file:${w.path}`] });
    toast.ok(t('watch.added', { name: w.name }));
  };
  const removeWatcher = (path: string) => {
    const w = watchers.find((x) => x.path === path);
    if (!w) return;
    void set('logs.watchers', watchers.filter((x) => x.path !== path));
    if (filter.sources.includes(`file:${path}`)) setFilter({ sources: ['all'] });
    toast.undo(t('sources.removed', { name: w.name }), t('sources.undo'), () => void set('logs.watchers', watchers));
  };

  const [sheet, setSheet] = useState(false);
  const pickSources = (ids: string[]) => {
    setFilter({ sources: ids });
    if (!wide) setSheet(false);
  };
  const sourcesLabel = useMemo(() => {
    if (filter.sources.length === 1 && filter.sources[0] === 'all') return t('sources.all');
    if (filter.sources.length > 1) return `${filter.sources.length} ${t('sources.title').toLowerCase()}`;
    const id = filter.sources[0];
    for (const g of sources?.groups ?? []) {
      const s = g.sources.find((x) => x.id === id);
      if (s) return s.label;
    }
    return id.replace(/^(unit|file):/, '').replace(/\.service$/, '').replace(/^.*\//, '');
  }, [filter.sources, sources, t]);

  const sourcesEl = (
    <Sources
      data={sources}
      error={srcError}
      selected={filter.sources}
      watchers={watchers}
      onSelect={pickSources}
      onAddWatcher={addWatcher}
      onRemoveWatcher={removeWatcher}
      onUnlock={unlockAndReload}
      onRetry={() => loadSources()}
      canUnlock={!!session?.canSudo}
    />
  );

  usePaletteActions('logs', [
    { id: 'logs.errors', title: t('palette.errors'), hint: t('title'), icon: 'alert', hue: 'log', keywords: ['error', 'errori'], run: () => { nav('/logs'); setFilter({ levels: ['err'] }); } },
    { id: 'logs.all', title: t('palette.all'), hint: t('title'), icon: 'logs', hue: 'log', run: () => setFilter({ levels: [...LEVELS] }) },
    { id: 'logs.live', title: t('palette.live'), hint: t('title'), icon: 'play', hue: 'log', keywords: ['follow', 'tail'], run: () => setFilter({ live: !filter.live }) },
  ]);

  const clearFilters = () => setParams(new URLSearchParams(), { replace: true });

  return (
    <Page flush hue="log">
      <div className="logs">
        {wide && sourcesEl}
        <div className="logs-main">
          <FilterBar
            filter={filter}
            counts={counts}
            onChange={setFilter}
            onRefresh={() => setTick((n) => n + 1)}
            onSources={() => setSheet(true)}
            sourcesLabel={sourcesLabel}
            showSourcesButton={!wide}
          />
          <Histogram
            data={hist}
            levels={filter.levels}
            loading={histLoading}
            onZoom={(from, to) => setFilter({ range: 'custom', from, to, live: false })}
          />
          {filter.range === 'custom' && (
            <div className="logs-zoomed">
              <Badge tone="info">{t('range.zoomed')}</Badge>
              <Button size="sm" variant="ghost" onClick={() => setFilter({ range: '1h', from: undefined, to: undefined })}>{t('range.reset')}</Button>
            </div>
          )}
          {skipped > 0 && !admin && (
            <div className="logs-skipped" role="status">
              <span>{t('stream.skipped', { count: skipped })}</span>
              <Button size="sm" onClick={unlockAndReload}>{t('stream.unlock')}</Button>
            </div>
          )}
          <div className="logs-body">
            <Stream
              rows={rows}
              selectedKey={sel}
              onSelect={(e) => setSel(e ? e.key : null)}
              loading={loading}
              loadingMore={moreLoading}
              hasMore={hasMore}
              onMore={loadMore}
              error={error}
              onRetry={() => (rows.length ? loadMore() : setTick((n) => n + 1))}
              onClear={clearFilters}
              withDate={spanMs(filter, anchor) > 20 * 3_600_000}
              mobile={mobile}
              live={filter.live}
            />
            <div className={inlineDetails ? 'logs-detwrap' : undefined}>
              <Details
                entry={selected}
                inline={inlineDetails}
                contextSource={contextSource}
                watchers={watchers}
                call_={lcall}
                onClose={() => setSel(null)}
                onOpenFile={(p) => nav(`/files?path=${encodeURIComponent(p)}`)}
                onGoService={(u) => nav(`/services?unit=${encodeURIComponent(u)}`)}
                onOnlySource={(id) => setFilter({ sources: [id] })}
              />
            </div>
          </div>
        </div>
      </div>
      {!wide && (
        <Sheet open={sheet} onClose={() => setSheet(false)} title={t('sources.title')}>
          {sourcesEl}
        </Sheet>
      )}
    </Page>
  );
}

