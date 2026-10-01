import { useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, call } from '../../api';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, Chip, EmptyState, Icon, Input, Skeleton, type IconName } from '../../ui';
import { AppIcon } from './AppIcon';
import { SourceBadge, displayName } from './helpers';
import { useSoftware } from './store';
import type { App, Kind, SearchResult, Update } from './types';

const CATEGORIES: { id: string; icon: IconName; hue: string }[] = [
  { id: 'development', icon: 'code', hue: 'term' },
  { id: 'internet', icon: 'globe', hue: 'file' },
  { id: 'graphics', icon: 'image', hue: 'plg' },
  { id: 'media', icon: 'video', hue: 'svc' },
  { id: 'system', icon: 'cog', hue: 'ov' },
  { id: 'servers', icon: 'server', hue: 'log' },
];

export interface OpenItem {
  name: string;
  title?: string;
  kind: Kind;
  source: string;
  installed: boolean;
  icon?: string;
}

interface Props {
  actions: {
    busy: boolean;
    updateSome(list: Update[]): void;
    updateAll(): void;
    askInstall(r: SearchResult): void;
    openTerminal(): void;
  };
  onOpen(item: OpenItem): void;
  goInstalled(): void;
}

const SHOWN_APPS = 8;

export function StoreTab({ actions, onOpen, goInstalled }: Props) {
  const t = useT('software');
  const { host } = useSession();
  const { summary, updates, installed, apps } = useSoftware();
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  const [category, setCategory] = useState<string | null>(null);
  const [filters, setFilters] = useState<Set<Kind>>(new Set());
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [popular, setPopular] = useState<SearchResult[] | null>(null);
  const [error, setError] = useState('');
  const [showAllApps, setShowAllApps] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(q.trim()), 350);
    return () => window.clearTimeout(id);
  }, [q]);

  // Search
  useEffect(() => {
    if (debounced.length < 2) {
      setResults(null);
      setError('');
      return;
    }
    const n = ++seq.current;
    setResults(null);
    setError('');
    call<SearchResult[]>('software.search', { query: debounced })
      .then((r) => n === seq.current && setResults(r))
      .catch((e) => n === seq.current && (setError(e instanceof ApiError ? e.message : String(e)), setResults([])));
  }, [debounced]);

  // Suggestions per category
  useEffect(() => {
    setPopular(null);
    call<SearchResult[]>('software.suggest', { category: category ?? '' })
      .then(setPopular)
      .catch(() => setPopular([]));
  }, [category]);

  const updateByName = useMemo(() => {
    const m = new Map<string, Update>();
    for (const u of updates ?? []) m.set(`${u.kind}:${u.name}`, u);
    return m;
  }, [updates]);

  const appByPkg = useMemo(() => {
    const m = new Map<string, App>();
    for (const a of apps ?? []) if (a.package) m.set(`${a.kind === 'aur' ? 'repo' : a.kind}:${a.package}`, a);
    return m;
  }, [apps]);

  const yourApps = useMemo(() => {
    const list = (apps ?? []).filter((a) => a.package && a.explicit);
    const upd = (a: App) => updateByName.has(`${a.kind}:${a.package}`) ? 0 : 1;
    return [...list].sort((a, b) => upd(a) - upd(b) || a.name.localeCompare(b.name));
  }, [apps, updateByName]);
  const withUpdates = yourApps.filter((a) => updateByName.has(`${a.kind}:${a.package}`));
  const appsShown = showAllApps ? yourApps : yourApps.slice(0, SHOWN_APPS);

  const available = summary?.sources ?? [];
  const visibleResults = (results ?? []).filter((r) => filters.size === 0 || filters.has(r.kind));
  const toggleFilter = (k: Kind) =>
    setFilters((f) => {
      const n = new Set(f);
      if (n.has(k)) n.delete(k);
      else n.add(k);
      return n;
    });

  const distro = host?.distro?.name || 'Linux';
  const explicitCount = (installed ?? []).filter((p) => p.reason === 'explicit').length;

  const resultCard = (r: SearchResult) => {
    const app = appByPkg.get(`${r.kind === 'aur' ? 'repo' : r.kind}:${r.name}`);
    const upd = updateByName.get(`${r.kind}:${r.name}`);
    return (
      <div key={`${r.kind}:${r.name}`} className="sw-ap">
        <button type="button" className="sw-ap-main" onClick={() => onOpen({ name: r.name, title: r.title, kind: r.kind, source: r.source, installed: r.installed, icon: app?.icon })}>
          <AppIcon icon={app?.icon} label={displayName(r)} size={48} />
          <span className="sw-ap-tx">
            <b>{displayName(r)}</b>
            <small>{r.description}</small>
            <span className="sw-ap-m">
              <SourceBadge kind={r.kind} source={r.source} />
              <span className="sw-ver">{r.version}</span>
            </span>
          </span>
        </button>
        {upd ? (
          <Button size="sm" variant="primary" disabled={actions.busy} onClick={() => actions.updateSome([upd])}>{t('update')}</Button>
        ) : r.installed ? (
          <Badge tone="ok">{t('installedBadge')}</Badge>
        ) : r.kind === 'aur' ? (
          <Button size="sm" icon="terminal" onClick={actions.openTerminal}>{t('terminal')}</Button>
        ) : (
          <Button size="sm" variant="primary" disabled={actions.busy} onClick={() => actions.askInstall(r)}>{t('install.button')}</Button>
        )}
      </div>
    );
  };

  const appCard = (a: App) => {
    const upd = updateByName.get(`${a.kind}:${a.package}`);
    return (
      <div key={a.id} className="sw-ap">
        <button type="button" className="sw-ap-main" onClick={() => onOpen({ name: a.package, title: a.name, kind: a.kind, source: a.source, installed: true, icon: a.icon })}>
          <AppIcon icon={a.icon} label={a.name} size={48} />
          <span className="sw-ap-tx">
            <b>{a.name}</b>
            <small>{a.comment}</small>
            <span className="sw-ap-m">
              <SourceBadge kind={a.kind} source={a.source} />
              <span className="sw-ver">{a.version.replace(/-\d+$/, '')}</span>
            </span>
          </span>
        </button>
        {upd ? (
          upd.kind === 'aur' ? (
            <Button size="sm" icon="terminal" onClick={actions.openTerminal}>{t('terminal')}</Button>
          ) : (
            <Button size="sm" variant="primary" disabled={actions.busy} onClick={() => actions.updateSome([upd])}>{t('update')}</Button>
          )
        ) : null}
      </div>
    );
  };

  const searching = debounced.length >= 2;

  return (
    <div className="sw-body">
      <div className="sw-big">
        <Input
          icon="search"
          placeholder={t('store.search')}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          aria-label={t('store.search')}
          autoFocus={false}
          end={
            available.length > 0 ? (
              <span className="sw-srcs">
                {available.map((k) => (
                  <Chip key={k} pressed={filters.has(k)} onClick={() => toggleFilter(k)}>
                    {t(`store.src.${k}`)}
                  </Chip>
                ))}
              </span>
            ) : undefined
          }
        />
      </div>

      <div className="sw-cats">
        {CATEGORIES.map((c) => (
          <button
            key={c.id}
            type="button"
            className={`sw-cat hue-${c.hue}${category === c.id ? ' sw-cat--on' : ''}`}
            aria-pressed={category === c.id}
            onClick={() => {
              setCategory(category === c.id ? null : c.id);
              setQ('');
            }}
          >
            <span className="sw-cat-ic"><Icon name={c.icon} /></span>
            {t(`store.cat.${c.id}`)}
          </button>
        ))}
      </div>

      {searching ? (
        <>
          <div className="sw-sec"><h3>{t('store.results')}</h3>{results && <span className="sw-muted">{visibleResults.length}</span>}</div>
          {results === null ? (
            <div className="sw-apps">{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={92} style={{ borderRadius: 18 }} />)}</div>
          ) : visibleResults.length === 0 ? (
            <EmptyState icon="search" hue="sw" title={error ? t('error.search') : t('store.none', { query: debounced })} text={error || t('store.noneText')} />
          ) : (
            <div className="sw-apps">{visibleResults.map(resultCard)}</div>
          )}
        </>
      ) : (
        <>
          {yourApps.length > 0 && !category && (
            <>
              <div className="sw-sec">
                <h3>{t('store.yourApps')}</h3>
                {withUpdates.length > 0 && <span className="sw-muted">{t('store.withUpdates', { count: withUpdates.length })}</span>}
                {withUpdates.length > 0 && (
                  <button type="button" className="sw-link" disabled={actions.busy} onClick={actions.updateAll}>{t('updateAll')}</button>
                )}
              </div>
              <div className="sw-apps">{appsShown.map(appCard)}</div>
              {yourApps.length > SHOWN_APPS && (
                <button type="button" className="sw-more" onClick={() => setShowAllApps((v) => !v)}>
                  {showAllApps ? t('showLess') : t('store.showAll', { count: yourApps.length })}
                </button>
              )}
            </>
          )}
          <div className="sw-sec"><h3>{category ? t(`store.cat.${category}`) : t('store.popular', { distro })}</h3></div>
          {popular === null ? (
            <div className="sw-apps">{Array.from({ length: 4 }, (_, i) => <Skeleton key={i} height={92} style={{ borderRadius: 18 }} />)}</div>
          ) : popular.length === 0 ? (
            <EmptyState icon="info" hue="sw" title={t('store.noSuggest')} />
          ) : (
            <div className="sw-apps">{popular.filter((r) => filters.size === 0 || filters.has(r.kind)).map(resultCard)}</div>
          )}
        </>
      )}

      {installed && (
        <div className="sw-pk">
          <span className="sw-pk-ic"><Icon name="software" /></span>
          <div>
            <b>{t('store.installedCount', { count: installed.length })}</b>
            <small>{t('store.installedText', { count: explicitCount })}</small>
          </div>
          <Button icon="list" onClick={goInstalled}>{t('store.allPackages')}</Button>
        </div>
      )}
    </div>
  );
}
