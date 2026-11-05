import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { PanelTab } from './types';

export type Tab = 'all' | 'running' | 'failed' | 'stopped' | 'timers' | 'sockets';
const TAB_IDS: Tab[] = ['all', 'running', 'failed', 'stopped', 'timers', 'sockets'];

/** Page state kept in the URL: the open unit, its panel tab and the list tab. */
export function useServicesRoute(win: boolean) {
  const [params, setParams] = useSearchParams();

  // ?unit=nginx or ?unit=nginx.service (links from Overview / Logs); ?state=failed|running|… picks the tab.
  const unitParam = params.get('unit');
  const selected = unitParam
    ? win || /\.[a-z]+$/.test(unitParam)
      ? unitParam
      : `${unitParam}.service`
    : null;
  const rawTab = (params.get('tab') as PanelTab | null) ?? 'info';
  const panelTab: PanelTab = win && (rawTab === 'logs' || rawTab === 'unit') ? 'info' : rawTab;

  const stateParam = params.get('state');
  const tabOk = (v: string | null): v is Tab =>
    TAB_IDS.includes(v as Tab) && !(win && (v === 'timers' || v === 'sockets'));
  const [tab, setTab] = useState<Tab>(() => (tabOk(stateParam) ? stateParam : 'all'));
  useEffect(() => {
    if (tabOk(stateParam)) setTab(stateParam);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stateParam, win]);

  const openUnit = (name: string, tabId: PanelTab = 'info', replace = false) => {
    const p = new URLSearchParams(params);
    p.set('unit', name);
    if (tabId === 'info') p.delete('tab');
    else p.set('tab', tabId);
    setParams(p, { replace });
  };
  const closePanel = () => {
    const p = new URLSearchParams(params);
    p.delete('unit');
    p.delete('tab');
    setParams(p, { replace: true });
  };
  const setPanelTab = (tb: PanelTab) => openUnit(selected ?? '', tb, true);

  return { selected, panelTab, tab, setTab, openUnit, closePanel, setPanelTab };
}
