import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast, Button, EmptyState, Page, Tabs, type TabItem } from '../../ui';
import { useI18n, useT } from '../../i18n';
import { relativeTime } from '../../lib/format';
import { useActions } from './actions';
import { HistoryTab } from './HistoryTab';
import { InstalledTab } from './InstalledTab';
import { PkgPanel, TxPanel, type PkgTarget } from './SidePanel';
import { StoreTab, type OpenItem } from './StoreTab';
import { UpdatesTab } from './UpdatesTab';
import { checkNow, loadApps, loadInstalled, loadSummary, loadUpdates, useSoftware } from './store';
import { attachIfBusy, closePanel, registerRuntime, useTx } from './tx';
import type { Detail, Pkg, SearchResult } from './types';
import './software.css';

type Tab = 'updates' | 'store' | 'installed' | 'history';
const TABS: Tab[] = ['updates', 'store', 'installed', 'history'];

export default function SoftwarePage() {
  const t = useT('software');
  const { lang } = useI18n();
  const nav = useNavigate();
  const [params, setParams] = useSearchParams();
  const tab: Tab = TABS.includes(params.get('tab') as Tab) ? (params.get('tab') as Tab) : 'updates';
  const { summary, updates, installed, apps, checking, error } = useSoftware();
  const tx = useTx();
  const acts = useActions();
  const [pkg, setPkg] = useState<PkgTarget | null>(null);

  useEffect(() => {
    registerRuntime(t, nav);
  }, [t, nav]);

  useEffect(() => {
    void loadSummary();
    void loadUpdates();
    void loadInstalled();
    void loadApps();
    void attachIfBusy();
  }, []);

  const setTab = (id: Tab) => {
    setParams(id === 'updates' ? {} : { tab: id }, { replace: true });
  };

  // One side panel at a time: starting a transaction shows it, opening a package hides it.
  useEffect(() => {
    if (tx.panelOpen) setPkg(null);
  }, [tx.panelOpen]);
  const openPkg = useCallback((p: PkgTarget) => {
    closePanel();
    setPkg(p);
  }, []);

  const iconFor = useCallback((name: string) => apps?.find((a) => a.package === name)?.icon, [apps]);

  const openFromStore = (i: OpenItem) => openPkg({ ...i, icon: i.icon ?? iconFor(i.name) });
  const openFromTable = (p: Pkg) => openPkg({ name: p.name, title: p.title, kind: p.kind, source: p.source, installed: true, icon: iconFor(p.name), scope: p.scope });

  const pkgUpdate = pkg ? (updates ?? []).find((u) => u.name === pkg.name && u.kind === pkg.kind) : undefined;

  const onInstallFromPanel = (target: PkgTarget, d: Detail | null) => {
    const origin = d?.fields.find((f) => f.key === 'Origin' || f.key === 'Remotes')?.value;
    const r: SearchResult = {
      name: target.name, title: target.title, kind: target.kind, source: target.source, version: d?.version ?? '',
      description: d?.description ?? '', installed: false, remote: target.kind === 'flatpak' ? origin : undefined,
    };
    setPkg(null);
    acts.askInstall(r);
  };

  const onRemoveFromPanel = (target: PkgTarget) => {
    const p = (installed ?? []).find((x) => x.name === target.name && x.kind === target.kind);
    if (!p) return toast.err(t('error.notInstalled', { name: target.name }));
    setPkg(null);
    acts.askRemove([p]);
  };

  const doCheck = async () => {
    try {
      const warnings = await checkNow();
      if (warnings.length) toast.info(t('check.warning'), warnings[0]);
      else toast.ok(t('check.done'));
    } catch (e) {
      toast.err(t('check.failed'), e instanceof Error ? e.message : String(e));
    }
  };

  const items: TabItem<Tab>[] = useMemo(
    () => [
      { id: 'updates', label: t('tab.updates'), count: summary ? summary.updates : undefined },
      { id: 'store', label: t('tab.store') },
      { id: 'installed', label: t('tab.installed'), count: installed?.length },
      { id: 'history', label: t('tab.history') },
    ],
    [t, summary, installed],
  );

  const unsupported = error?.code === 'unavailable' && !summary;

  return (
    <Page flush hue="sw">
      <div className="sw-root hue-sw">
        <div className="sw-bar">
          <Tabs variant="pill" hue="sw" items={items} value={tab} onChange={setTab} aria-label={t('title')} />
          <span className="sw-sp" />
          {summary && summary.lastCheck > 0 && <span className="sw-muted sw-checked">{t('checked', { when: relativeTime(summary.lastCheck, lang) })}</span>}
          <Button icon="refresh" loading={checking} onClick={() => void doCheck()}>{t('checkNow')}</Button>
        </div>
        {unsupported ? (
          <EmptyState icon="software" hue="sw" title={t('unsupported.title')} text={error?.message} />
        ) : tab === 'updates' ? (
          <UpdatesTab actions={acts} />
        ) : tab === 'store' ? (
          <StoreTab actions={acts} onOpen={openFromStore} goInstalled={() => setTab('installed')} />
        ) : tab === 'installed' ? (
          <InstalledTab activeKey={pkg ? `${pkg.kind}:${pkg.scope ?? ''}:${pkg.name}` : undefined} onOpen={openFromTable} onRemove={acts.askRemove} busy={acts.busy} />
        ) : (
          <HistoryTab reloadKey={tx.phase === 'done' ? tx.startedAt : 0} />
        )}
      </div>
      <TxPanel tx={tx} openTerminal={acts.openTerminal} />
      <PkgPanel
        target={pkg}
        update={pkgUpdate}
        busy={acts.busy}
        onClose={() => setPkg(null)}
        onInstall={onInstallFromPanel}
        onRemove={onRemoveFromPanel}
        onUpdate={(u) => {
          setPkg(null);
          acts.updateSome([u]);
        }}
        openTerminal={acts.openTerminal}
      />
      {acts.ui}
    </Page>
  );
}
