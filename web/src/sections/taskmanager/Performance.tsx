import { useT } from '../../i18n';
import { formatBytes, formatDuration } from '../../lib/format';
import { AreaChart, Icon, Progress, type IconName } from '../../ui';
import { physicalNet, type Host, type Metrics, type Point } from '../overview/data';
import type { TaskSnapshot } from './model';

const percentage = (value: number | null | undefined) =>
  value == null ? '—' : `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })}%`;

export function SummaryTiles({
  metrics,
  snapshot,
}: {
  metrics: Metrics | null;
  snapshot: TaskSnapshot | null;
}) {
  const t = useT('taskmanager');
  const network = metrics
    ? physicalNet(metrics).reduce((sum, n) => sum + n.rxRate + n.txRate, 0)
    : null;
  const gpu = snapshot?.capabilities.gpu.supported
    ? snapshot.gpus.find((g) => g.utilization !== null)?.utilization
    : null;
  const items: { label: string; value: string; detail: string; icon: IconName; hue: string }[] = [
    {
      label: t('cpu'),
      value: percentage(metrics?.cpu.percent),
      detail: t('hostCpu'),
      icon: 'cpu',
      hue: 'ov',
    },
    {
      label: t('memory'),
      value: percentage(metrics?.memory.percent),
      detail: metrics
        ? `${formatBytes(metrics.memory.used)} / ${formatBytes(metrics.memory.total)}`
        : t('waiting'),
      icon: 'tm-memory',
      hue: 'plg',
    },
    {
      label: t('network'),
      value: network === null ? '—' : `${formatBytes(network)}/s`,
      detail: t('hostAggregate'),
      icon: 'net',
      hue: 'term',
    },
    {
      label: t('gpu'),
      value: percentage(gpu),
      detail: snapshot?.gpus[0]?.name ?? t('gpuUnavailable'),
      icon: 'grid',
      hue: 'log',
    },
  ];
  return (
    <div className="tm-summary">
      {items.map((item) => (
        <div key={item.hue} className={`tm-summary-tile hue-${item.hue}`}>
          <Icon name={item.icon} />
          <div>
            <small>{item.label}</small>
            <strong>{item.value}</strong>
            <span title={item.detail}>{item.detail}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

export function Performance({
  metrics,
  history,
  host,
  snapshot,
}: {
  metrics: Metrics | null;
  history: Point[];
  host: Host | null;
  snapshot: TaskSnapshot | null;
}) {
  const t = useT('taskmanager');
  const points = history.slice(-60);
  const interfaces = metrics ? physicalNet(metrics) : [];
  const cardHeading = (title: string, value: string, subtitle: string) => (
    <header>
      <div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
      <strong>{value}</strong>
    </header>
  );
  return (
    <div className="tm-performance">
      <section className="tm-performance-card hue-ov">
        {cardHeading(t('cpu'), percentage(metrics?.cpu.percent), host?.cpu.model ?? t('hostCpu'))}
        <div className="tm-graph">
          <AreaChart
            label={t('cpuGraph')}
            series={[{ values: points.map((p) => p.cpu), color: 'var(--h-ov)' }]}
            height={170}
            min={0}
            max={100}
          />
          <span>100%</span>
        </div>
        <div className="tm-graph-caption">
          <span>{t('recentSamples')}</span>
          <span>0</span>
        </div>
        <dl className="tm-facts">
          <div>
            <dt>{t('logicalProcessors')}</dt>
            <dd>{host?.cpu.threads ?? snapshot?.cpuCores ?? '—'}</dd>
          </div>
          <div>
            <dt>{t('processes')}</dt>
            <dd>{snapshot?.processes.length ?? '—'}</dd>
          </div>
          <div>
            <dt>{t('uptime')}</dt>
            <dd>{host ? formatDuration(host.uptime) : '—'}</dd>
          </div>
        </dl>
      </section>
      <section className="tm-performance-card hue-plg">
        {cardHeading(
          t('memory'),
          percentage(metrics?.memory.percent),
          metrics
            ? t('memoryCapacity', { total: formatBytes(metrics.memory.total) })
            : t('waiting'),
        )}
        <div className="tm-graph">
          <AreaChart
            label={t('memoryGraph')}
            series={[{ values: points.map((p) => p.mem), color: 'var(--h-plg)' }]}
            height={170}
            min={0}
            max={100}
          />
          <span>100%</span>
        </div>
        <div className="tm-graph-caption">
          <span>{t('recentSamples')}</span>
          <span>0</span>
        </div>
        <dl className="tm-facts">
          <div>
            <dt>{t('inUse')}</dt>
            <dd>{metrics ? formatBytes(metrics.memory.used) : '—'}</dd>
          </div>
          <div>
            <dt>{t('available')}</dt>
            <dd>{metrics ? formatBytes(metrics.memory.available) : '—'}</dd>
          </div>
          <div>
            <dt>{t('swap')}</dt>
            <dd>
              {metrics
                ? `${formatBytes(metrics.swap.used)} / ${formatBytes(metrics.swap.total)}`
                : '—'}
            </dd>
          </div>
        </dl>
      </section>
      <section className="tm-performance-card hue-term">
        {cardHeading(
          t('network'),
          metrics
            ? `${formatBytes(interfaces.reduce((sum, n) => sum + n.rxRate + n.txRate, 0))}/s`
            : '—',
          t('hostAggregate'),
        )}
        <div className="tm-graph">
          <AreaChart
            label={t('networkGraph')}
            series={[{ values: points.map((p) => p.net), color: 'var(--h-term)' }]}
            height={150}
            min={0}
          />
        </div>
        <p className="tm-network-note">{t('networkScope')}</p>
        <div className="tm-interfaces">
          {interfaces.map((network) => (
            <div key={network.iface}>
              <span>
                <Icon name="net" />
                {network.iface}
              </span>
              <small>↓ {formatBytes(network.rxRate)}/s</small>
              <small>↑ {formatBytes(network.txRate)}/s</small>
            </div>
          ))}
        </div>
      </section>
      <section className="tm-performance-card hue-log">
        {cardHeading(
          t('gpu'),
          snapshot?.capabilities.gpu.supported
            ? t('gpuDeviceCount', { count: snapshot.gpus.length })
            : t('unavailableShort'),
          t('gpuHost'),
        )}
        {!snapshot?.capabilities.gpu.supported || !snapshot.gpus.length ? (
          <div className="tm-gpu-unavailable">
            <Icon name="grid" />
            <b>{t('gpuUnavailable')}</b>
            <p>{t('gpuUnavailableDetail')}</p>
          </div>
        ) : (
          snapshot.gpus.map((gpu) => (
            <div className="tm-gpu" key={gpu.id}>
              <b>{gpu.name}</b>
              <strong>{percentage(gpu.utilization)}</strong>
              {gpu.utilization !== null && (
                <Progress value={gpu.utilization} hue="log" label={gpu.name} />
              )}
              <small>
                {gpu.memoryUsed !== null && gpu.memoryTotal !== null
                  ? t('gpuMemory', {
                      used: formatBytes(gpu.memoryUsed),
                      total: formatBytes(gpu.memoryTotal),
                    })
                  : t('gpuMemoryUnavailable')}
              </small>
            </div>
          ))
        )}
      </section>
    </div>
  );
}
