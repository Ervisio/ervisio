import { useState } from 'react';
import { usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import { formatBytes, formatTime } from '../../lib/format';
import { RefreshSelect } from '../../lib/RefreshSelect';
import { refreshNow, useSetRefreshInterval } from '../../lib/refresh';
import { WindowActivityProvider, useWindowActivity } from '../../lib/windowActivity';
import { useAccountScope } from '../../lib/accountScope';
import { Button, Dialog, EmptyState, Icon, Input, registerIcon, Skeleton, Tabs } from '../../ui';
import { NativeAppFrame } from '../../ui/NativeAppFrame';
import { useHost, useMetrics } from '../overview/data';
import { useTaskSnapshot } from './data';
import { machineCpu, type TaskProcess } from './model';
import { Performance, SummaryTiles } from './Performance';
import { Processes } from './Processes';
import './taskmanager.css';

type Tab = 'processes' | 'performance';

registerIcon(
  'tm-memory',
  '<rect x="3" y="6" width="18" height="11" rx="1"/><path d="M7 17v3M11 17v3M15 17v3M19 17v3M7 9v5M11 9v5M15 9v5M19 9v5"/>',
);
registerIcon('tm-performance', '<path d="M3 3v18h18M6 15l4-5 4 3 6-8"/>');

export default function TaskManagerPage() {
  const [paused, setPaused] = useState(false);
  const scope = useAccountScope();
  return (
    <WindowActivityProvider active={!paused}>
      <TaskManager key={scope} paused={paused} onPause={() => setPaused((value) => !value)} />
    </WindowActivityProvider>
  );
}

function TaskManager({ paused, onPause }: { paused: boolean; onPause(): void }) {
  const t = useT('taskmanager');
  const { host: publicHost } = useSession();
  const { prefs } = usePrefs();
  const desktop = (prefs['shell.mode'] ?? publicHost?.defaultShell) === 'ervisios';
  const [tab, setTab] = useState<Tab>('processes');
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<TaskProcess | null>(null);
  const active = useWindowActivity();
  const data = useTaskSnapshot();
  const { metrics, history, error: metricsError } = useMetrics();
  const host = useHost();
  const setInterval = useSetRefreshInterval();
  const selectedProcess =
    data.snapshot?.processes.find((process) => process.pid === selected?.pid) ?? selected;
  const cpu =
    selectedProcess && data.snapshot ? machineCpu(selectedProcess, data.snapshot.cpuCores) : null;
  return (
    <div className="tm-root hue-ov">
      <NativeAppFrame<Tab>
        title={t('title')}
        icon="overview"
        items={[
          { id: 'processes' as const, label: t('processes'), icon: 'list' },
          { id: 'performance' as const, label: t('performance'), icon: 'tm-performance' },
        ]}
        active={tab}
        onSelect={setTab}
      >
        <div className="tm-layout">
          {!desktop && (
            <header className="tm-classic-header">
              <h1 className="title">{t('title')}</h1>
              <Tabs<Tab>
                aria-label={t('title')}
                items={[
                  { id: 'processes', label: t('processes'), icon: 'list' },
                  { id: 'performance', label: t('performance'), icon: 'tm-performance' },
                ]}
                value={tab}
                onChange={setTab}
              />
            </header>
          )}
          <div className="tm-toolbar">
            <div className="tm-search">
              <Input
                icon="search"
                aria-label={t('search')}
                placeholder={t('search')}
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value);
                  if (tab !== 'processes') setTab('processes');
                }}
              />
            </div>
            <Button
              size="sm"
              icon={paused ? 'play' : 'pause'}
              aria-pressed={paused}
              onClick={onPause}
            >
              {t(paused ? 'resume' : 'pause')}
            </Button>
            <Button
              size="sm"
              icon="refresh"
              disabled={!active}
              onClick={() => {
                data.refresh();
                refreshNow();
              }}
            >
              {t('refresh')}
            </Button>
            <div className="tm-refresh-rate">
              <RefreshSelect label={t('refreshRate')} onPick={(value) => void setInterval(value)} />
            </div>
          </div>
          <SummaryTiles metrics={metrics} snapshot={data.snapshot} />
          <div className="tm-host-caption">
            <Icon name="server" />
            <span>
              {publicHost?.hostname ?? host?.hostname} · {t('hostTelemetry')}
            </span>
          </div>
          {data.error && (
            <div className="tm-error" role="status">
              <Icon name="alert" />
              <span>
                {t('loadFailed')} {data.error}
              </span>
              <Button size="sm" disabled={!active} onClick={data.refresh}>
                {t('retry')}
              </Button>
            </div>
          )}
          {metricsError && (
            <div className="tm-error" role="status">
              <Icon name="alert" />
              <span>
                {t('metricsFailed')} {metricsError}
              </span>
            </div>
          )}
          <div className="tm-content">
            {tab === 'performance' ? (
              <Performance
                metrics={metrics}
                history={history}
                host={host}
                snapshot={data.snapshot}
              />
            ) : data.loading && !data.snapshot ? (
              <div className="tm-loading">
                <Skeleton lines={10} height={30} />
              </div>
            ) : data.snapshot ? (
              <Processes snapshot={data.snapshot} query={query} onInspect={setSelected} />
            ) : (
              <EmptyState icon="overview" title={t('loadFailed')} text={data.error} />
            )}
          </div>
          <footer className="tm-footer">
            <span>
              <i className={active && !data.error ? 'is-live' : ''} />
              {t(paused ? 'paused' : data.error ? 'stale' : 'live')}
            </span>
            <span>
              {data.snapshot
                ? t('processCount', { count: data.snapshot.processes.length })
                : t('waiting')}
            </span>
            <span>
              {data.snapshot
                ? t('updated', { time: formatTime(data.snapshot.time, { seconds: true }) })
                : ''}
            </span>
          </footer>
        </div>
        <Dialog
          open={!!selectedProcess}
          onClose={() => setSelected(null)}
          title={selectedProcess?.name ?? t('details')}
          icon="terminal"
          footer={<Button onClick={() => setSelected(null)}>{t('close')}</Button>}
        >
          {selectedProcess && (
            <div className="tm-details">
              <dl>
                {[
                  [t('column.pid'), selectedProcess.pid],
                  [t('parentPid'), selectedProcess.ppid],
                  [t('column.user'), selectedProcess.user || t('unavailableShort')],
                  [t('threads'), selectedProcess.threads],
                  [
                    t('cpu'),
                    cpu === null
                      ? t('unavailableShort')
                      : `${cpu.toLocaleString(undefined, { maximumFractionDigits: 1 })}%`,
                  ],
                  [
                    t('memory'),
                    selectedProcess.memoryAvailable
                      ? formatBytes(selectedProcess.memory)
                      : t('unavailableShort'),
                  ],
                ].map(([label, value]) => (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              </dl>
              <h3>{t('command')}</h3>
              <pre>{selectedProcess.command || t('unavailableShort')}</pre>
              <p>{t('protected')}</p>
            </div>
          )}
        </Dialog>
      </NativeAppFrame>
    </div>
  );
}
