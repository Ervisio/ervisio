import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { call } from '../../api';
import { useT } from '../../i18n';
import { Button, EmptyState, Input, Page, Skeleton, Tabs, toast, useIsMobile } from '../../ui';
import { useRailBadge } from '../index';
import { Browse } from './Browse';
import { PluginCard } from './Cards';
import { ConsentDialog } from './ConsentDialog';
import { errMsg, usePluginData } from './data';
import { DetailPanel } from './DetailPanel';
import { Developer } from './Developer';
import { Security } from './Security';
import type { CatalogEntry, PluginInfo, TabId } from './types';
import './plugins.css';

const TABS: TabId[] = ['installed', 'updates', 'browse', 'security', 'developer'];

export default function PluginsPage() {
  const t = useT('plugins');
  const mobile = useIsMobile();
  const [params, setParams] = useSearchParams();
  const raw = params.get('tab') as TabId | null;
  const tab: TabId = raw && TABS.includes(raw) ? raw : 'installed';
  const setTab = useCallback((id: TabId) => setParams(id === 'installed' ? {} : { tab: id }, { replace: true }), [setParams]);

  const { plugins, catalog, state, error, refresh } = usePluginData();
  const [query, setQuery] = useState('');
  const [sel, setSel] = useState<string | null>(null);
  const [busyId, setBusyId] = useState('');
  const [consent, setConsent] = useState<{ entry: CatalogEntry; mode: 'install' | 'update' } | null>(null);

  const withUpdate = useMemo(() => plugins.filter((p) => p.updateAvailable), [plugins]);
  useRailBadge('plugins', withUpdate.length, 'info');

  const q = query.trim().toLowerCase();
  const matches = (p: PluginInfo) => !q || `${p.name} ${p.description} ${p.author} ${p.id}`.toLowerCase().includes(q);
  const shown = (tab === 'updates' ? withUpdate : plugins).filter(matches);

  // Desktop: keep a plugin open in the side panel, like the design.
  useEffect(() => {
    if (tab !== 'installed' && tab !== 'updates') return;
    if (sel && shown.some((p) => p.id === sel)) return;
    setSel(mobile ? null : shown[0]?.id ?? null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, shown.map((p) => p.id).join(','), mobile]);

  const selected = plugins.find((p) => p.id === sel) ?? null;

  const toggle = async (p: PluginInfo, on: boolean) => {
    setBusyId(p.id);
    try {
      await call('plugins.setEnabled', { id: p.id, enabled: on });
      toast.ok(t(on ? 'enabled' : 'disabled', { name: p.name }));
      await refresh(true);
    } catch (e) {
      toast.err(t('toggleFailed', { name: p.name }), errMsg(e));
    } finally {
      setBusyId('');
    }
  };

  const runInstall = async (entry: CatalogEntry, mode: 'install' | 'update') => {
    if (!entry.source) {
      toast.err(t('installFailed', { name: entry.name }), t('noSource'));
      return;
    }
    setBusyId(entry.id);
    try {
      await call('plugins.install', { source: entry.source, sha256: entry.sha256 || undefined, consent: entry.capabilities });
      toast.ok(t(mode === 'install' ? 'installedToast' : 'updatedToast', { name: entry.name }));
      setConsent(null);
      await refresh(true);
      if (mode === 'install') setSel(entry.id);
    } catch (e) {
      toast.err(t('installFailed', { name: entry.name }), errMsg(e));
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

  const tabs = [
    { id: 'installed' as const, label: t('tabs.installed'), count: plugins.length },
    { id: 'updates' as const, label: t('tabs.updates'), count: withUpdate.length },
    { id: 'browse' as const, label: t('tabs.browse') },
    { id: 'security' as const, label: t('tabs.security') },
    { id: 'developer' as const, label: t('tabs.developer') },
  ];

  const list = (items: PluginInfo[], empty: { title: string; text: string }) =>
    state === 'loading' ? (
      <div className="plugins-pad"><Skeleton lines={5} /></div>
    ) : state === 'error' ? (
      <div className="plugins-pad"><EmptyState icon="alert" hue="svc" title={t('loadFailed')} text={error} action={<Button onClick={() => void refresh()}>{t('retry')}</Button>} /></div>
    ) : (
      <div className="plugins-split">
        <div className="plugins-main">
          {items.length === 0 ? (
            <EmptyState icon="plugins" hue="plg" title={empty.title} text={empty.text} action={tab === 'installed' ? <Button variant="primary" onClick={() => setTab('browse')}>{t('getPlugins')}</Button> : undefined} />
          ) : (
            <div className="plugins-grid">
              {items.map((p) => (
                <PluginCard key={p.id} p={p} active={p.id === sel} busy={busyId === p.id} onOpen={() => setSel(p.id)} onToggle={(on) => void toggle(p, on)} />
              ))}
              {tab === 'installed' && !q && (
                <button type="button" className="plugins-add" onClick={() => setTab('browse')}>
                  <span>+</span>
                  {t('browsePlugins')}
                </button>
              )}
            </div>
          )}
        </div>
        {selected && items.some((p) => p.id === selected.id) && (
          <DetailPanel p={selected} busy={busyId === selected.id} onClose={() => setSel(null)} onToggle={(on) => void toggle(selected, on)} onUpdate={startUpdate} refresh={refresh} />
        )}
      </div>
    );

  return (
    <Page flush hue="plg">
      <div className="plugins-root">
        <div className="plugins-bar">
          <Tabs<TabId> variant="pill" hue="plg" aria-label={t('title')} value={tab} onChange={setTab} items={tabs} />
          <span className="plugins-sp" />
          {(tab === 'installed' || tab === 'updates' || tab === 'browse') && (
            <div className="plugins-search"><Input icon="search" aria-label={t('search')} placeholder={t('search')} value={query} onChange={(e) => setQuery(e.target.value)} /></div>
          )}
          <Button variant="primary" icon="plus" onClick={() => setTab('browse')}>{t('getPlugins')}</Button>
        </div>
        <div className="plugins-body">
          {tab === 'installed' && list(shown, { title: q ? t('noMatch') : t('emptyInstalled'), text: q ? t('noMatchText') : t('emptyInstalledText') })}
          {tab === 'updates' && list(shown, { title: t('emptyUpdates'), text: t('emptyUpdatesText') })}
          {tab === 'browse' && <Browse catalog={catalog} query={query} busyId={busyId} onInstall={(e) => setConsent({ entry: e, mode: 'install' })} />}
          {tab === 'security' && (state === 'ready' ? <Security plugins={plugins} busyId={busyId} onToggle={(p, on) => void toggle(p, on)} /> : <div className="plugins-pad"><Skeleton lines={5} /></div>)}
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
  );
}
