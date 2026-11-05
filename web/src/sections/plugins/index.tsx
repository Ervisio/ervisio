import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { call, errorMessage } from '../../api';
import { useT } from '../../i18n';
import { Button, EmptyState, Input, Page, Skeleton, Tabs, toast, useIsMobile } from '../../ui';
import { useRailBadge } from '../index';
import { Browse } from './Browse';
import { MovedCard, PluginCard } from './Cards';
import { ConsentDialog } from './ConsentDialog';
import { usePluginData, type PluginData } from './data';
import { DetailPanel } from './DetailPanel';
import { Developer } from './Developer';
import { Security } from './Security';
import type { CatalogEntry, PluginInfo, TabId } from './types';
import './plugins.css';
import { NativeAppFrame } from '../../ui/NativeAppFrame';

const MOVED_DISMISSED = 'ervisio.plugins.movedDismissed';
const TABS: TabId[] = ['installed', 'updates', 'browse', 'security', 'developer'];

type ConsentMode = 'install' | 'update';
type ConsentRequest = { entry: CatalogEntry; mode: ConsentMode };
type EmptyText = { title: string; text: string };

function useTabParam() {
  const [params, setParams] = useSearchParams();
  const raw = params.get('tab') as TabId | null;
  const tab: TabId = raw && TABS.includes(raw) ? raw : 'installed';
  const setTab = useCallback(
    (id: TabId) => setParams(id === 'installed' ? {} : { tab: id }, { replace: true }),
    [setParams],
  );
  return [tab, setTab] as const;
}

function loadDismissed(): string[] {
  try {
    return JSON.parse(localStorage.getItem(MOVED_DISMISSED) ?? '[]') as string[];
  } catch {
    return [];
  }
}

/** Plugins that left Ervisio (Docker) and are not installed here; "Not now" is remembered in this browser. */
function useMovedPlugins(catalog: PluginData['catalog']) {
  const [dismissed, setDismissed] = useState<string[]>(loadDismissed);
  const moved = useMemo(
    () =>
      (catalog?.moved ?? [])
        .filter((m) => !dismissed.includes(m.id))
        .map((m) => catalog?.plugins.find((e) => e.id === m.id))
        .filter((e): e is CatalogEntry => !!e && !e.installed),
    [catalog, dismissed],
  );
  const dismissMoved = (id: string) => {
    const next = [...dismissed, id];
    setDismissed(next);
    try {
      localStorage.setItem(MOVED_DISMISSED, JSON.stringify(next));
    } catch {
      /* private mode: dismissed for this visit only */
    }
  };
  return { moved, dismissMoved };
}

const matchesQuery = (p: PluginInfo, q: string) =>
  !q || `${p.name} ${p.description} ${p.author} ${p.id}`.toLowerCase().includes(q);

function usePluginActions({
  catalog,
  refresh,
  setSel,
}: {
  catalog: PluginData['catalog'];
  refresh: PluginData['refresh'];
  setSel(id: string): void;
}) {
  const t = useT('plugins');
  const [busyId, setBusyId] = useState('');
  const [consent, setConsent] = useState<ConsentRequest | null>(null);

  const toggle = async (p: PluginInfo, on: boolean) => {
    setBusyId(p.id);
    try {
      await call('plugins.setEnabled', { id: p.id, enabled: on });
      toast.ok(t(on ? 'enabled' : 'disabled', { name: p.name }));
      await refresh(true);
    } catch (e) {
      toast.err(t('toggleFailed', { name: p.name }), errorMessage(e));
    } finally {
      setBusyId('');
    }
  };

  const runInstall = async (entry: CatalogEntry, mode: ConsentMode) => {
    if (!entry.source) {
      toast.err(t('installFailed', { name: entry.name }), t('noSource'));
      return;
    }
    setBusyId(entry.id);
    try {
      await call('plugins.install', {
        source: entry.source,
        sha256: entry.sha256 || undefined,
        consent: entry.capabilities,
      });
      toast.ok(t(mode === 'install' ? 'installedToast' : 'updatedToast', { name: entry.name }));
      setConsent(null);
      await refresh(true);
      if (mode === 'install') setSel(entry.id);
    } catch (e) {
      toast.err(t('installFailed', { name: entry.name }), errorMessage(e));
    } finally {
      setBusyId('');
    }
  };

  const startUpdate = (p: PluginInfo) => {
    const entry = catalog?.plugins.find((e) => e.id === p.id);
    if (!entry) {
      toast.err(t('installFailed', { name: p.name }), t('noSource'));
      return;
    }
    if (p.updateAvailable?.newPermissions) setConsent({ entry, mode: 'update' });
    else void runInstall(entry, 'update');
  };

  return { busyId, consent, setConsent, toggle, runInstall, startUpdate };
}

type PluginListProps = {
  data: PluginData;
  items: PluginInfo[];
  empty: EmptyText;
  isInstalledTab: boolean;
  showExtras: boolean;
  moved: CatalogEntry[];
  sel: string | null;
  selected: PluginInfo | null;
  busyId: string;
  onSelect(id: string | null): void;
  onToggle(p: PluginInfo, on: boolean): void;
  onUpdate(p: PluginInfo): void;
  onBrowse(): void;
  onInstallMoved(e: CatalogEntry): void;
  onDismissMoved(id: string): void;
};

function PluginList({
  data,
  items,
  empty,
  isInstalledTab,
  showExtras,
  moved,
  sel,
  selected,
  busyId,
  onSelect,
  onToggle,
  onUpdate,
  onBrowse,
  onInstallMoved,
  onDismissMoved,
}: PluginListProps) {
  const t = useT('plugins');
  if (data.state === 'loading')
    return (
      <div className="plugins-pad">
        <Skeleton lines={5} />
      </div>
    );
  if (data.state === 'error') {
    return (
      <div className="plugins-pad">
        <EmptyState
          icon="alert"
          hue="svc"
          title={t('loadFailed')}
          text={data.error}
          action={<Button onClick={() => void data.refresh()}>{t('retry')}</Button>}
        />
      </div>
    );
  }
  return (
    <div className="plugins-split">
      <div className="plugins-main">
        {showExtras &&
          moved.map((e) => (
            <MovedCard
              key={e.id}
              entry={e}
              busy={busyId === e.id}
              onInstall={() => onInstallMoved(e)}
              onDismiss={() => onDismissMoved(e.id)}
            />
          ))}
        {items.length === 0 ? (
          <EmptyState
            icon="plugins"
            hue="plg"
            title={empty.title}
            text={empty.text}
            action={
              isInstalledTab ? (
                <Button variant="primary" onClick={onBrowse}>
                  {t('getPlugins')}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <div className="plugins-grid">
            {items.map((p) => (
              <PluginCard
                key={p.id}
                p={p}
                active={p.id === sel}
                busy={busyId === p.id}
                onOpen={() => onSelect(p.id)}
                onToggle={(on) => onToggle(p, on)}
              />
            ))}
            {showExtras && (
              <button type="button" className="plugins-add" onClick={onBrowse}>
                <span>+</span>
                {t('browsePlugins')}
              </button>
            )}
          </div>
        )}
      </div>
      {selected && items.some((p) => p.id === selected.id) && (
        <DetailPanel
          p={selected}
          busy={busyId === selected.id}
          onClose={() => onSelect(null)}
          onToggle={(on) => onToggle(selected, on)}
          onUpdate={onUpdate}
          refresh={data.refresh}
        />
      )}
    </div>
  );
}

export default function PluginsPage() {
  const t = useT('plugins');
  const mobile = useIsMobile();
  const [tab, setTab] = useTabParam();

  const data = usePluginData();
  const { plugins, catalog, state, refresh } = data;
  const [query, setQuery] = useState('');
  const [sel, setSel] = useState<string | null>(null);
  const { busyId, consent, setConsent, toggle, runInstall, startUpdate } = usePluginActions({
    catalog,
    refresh,
    setSel,
  });
  const { moved, dismissMoved } = useMovedPlugins(catalog);

  const withUpdate = useMemo(() => plugins.filter((p) => p.updateAvailable), [plugins]);
  useRailBadge('plugins', withUpdate.length, 'info');

  const q = query.trim().toLowerCase();
  const shown = (tab === 'updates' ? withUpdate : plugins).filter((p) => matchesQuery(p, q));

  // Desktop: keep a plugin open in the side panel, like the design.
  useEffect(() => {
    if (tab !== 'installed' && tab !== 'updates') return;
    if (sel && shown.some((p) => p.id === sel)) return;
    setSel(mobile ? null : (shown[0]?.id ?? null));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, shown.map((p) => p.id).join(','), mobile]);

  const selected = plugins.find((p) => p.id === sel) ?? null;
  const askInstall = (entry: CatalogEntry) => setConsent({ entry, mode: 'install' });
  const onToggle = (p: PluginInfo, on: boolean) => void toggle(p, on);
  const browse = () => setTab('browse');
  const hasSearch = tab === 'installed' || tab === 'updates' || tab === 'browse';

  const tabs = [
    { id: 'installed' as const, label: t('tabs.installed'), count: plugins.length },
    { id: 'updates' as const, label: t('tabs.updates'), count: withUpdate.length },
    { id: 'browse' as const, label: t('tabs.browse') },
    { id: 'security' as const, label: t('tabs.security') },
    { id: 'developer' as const, label: t('tabs.developer') },
  ];

  const list = (empty: EmptyText) => (
    <PluginList
      data={data}
      items={shown}
      empty={empty}
      isInstalledTab={tab === 'installed'}
      showExtras={tab === 'installed' && !q}
      moved={moved}
      sel={sel}
      selected={selected}
      busyId={busyId}
      onSelect={setSel}
      onToggle={onToggle}
      onUpdate={startUpdate}
      onBrowse={browse}
      onInstallMoved={askInstall}
      onDismissMoved={dismissMoved}
    />
  );

  return (
    <NativeAppFrame
      title={t('title')}
      icon="plugins"
      items={tabs.map((item) => ({
        ...item,
        icon: (
          {
            installed: 'plugins',
            updates: 'download',
            browse: 'grid',
            security: 'shield',
            developer: 'code',
          } as const
        )[item.id],
      }))}
      active={tab}
      onSelect={setTab}
    >
      <Page flush hue="plg">
        <div className="plugins-root">
          <div className="plugins-bar">
            <Tabs<TabId>
              variant="pill"
              hue="plg"
              aria-label={t('title')}
              value={tab}
              onChange={setTab}
              items={tabs}
            />
            <span className="plugins-sp" />
            {hasSearch && (
              <div className="plugins-search">
                <Input
                  icon="search"
                  aria-label={t('search')}
                  placeholder={t('search')}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </div>
            )}
            <Button variant="primary" icon="plus" onClick={browse}>
              {t('getPlugins')}
            </Button>
          </div>
          <div className="plugins-body">
            {tab === 'installed' &&
              list({
                title: q ? t('noMatch') : t('emptyInstalled'),
                text: q ? t('noMatchText') : t('emptyInstalledText'),
              })}
            {tab === 'updates' && list({ title: t('emptyUpdates'), text: t('emptyUpdatesText') })}
            {tab === 'browse' && (
              <Browse catalog={catalog} query={query} busyId={busyId} onInstall={askInstall} />
            )}
            {tab === 'security' &&
              (state === 'ready' ? (
                <Security plugins={plugins} busyId={busyId} onToggle={onToggle} />
              ) : (
                <div className="plugins-pad">
                  <Skeleton lines={5} />
                </div>
              ))}
            {tab === 'developer' && <Developer plugins={plugins} refresh={refresh} />}
          </div>
        </div>
        <ConsentDialog
          entry={consent?.entry ?? null}
          mode={consent?.mode ?? 'install'}
          busy={!!consent && busyId === consent.entry.id}
          onCancel={() => setConsent(null)}
          onConfirm={() => consent && void runInstall(consent.entry, consent.mode)}
        />
      </Page>
    </NativeAppFrame>
  );
}
