import { useStats } from '../../api/hooks';
import { formatBytes, formatPercent, formatRate } from '../../api/format';
import { info } from '../../api/resources';
import type { ContainerInspect } from '../../api/types';
import { t } from '../../i18n';
import { peak, rateSeries, StatChart } from './StatChart';

/** The four live charts: CPU, memory, network, disk I/O. Used small on Overview and large on Stats. */
export function LiveCharts({ id, running, inspect, height }: { id: string; running: boolean; inspect: ContainerInspect; height: number }) {
  const { last, history } = useStats(id, running, 1000);
  const ncpu = info.use().data?.info.NCPU || 0;
  const limit = inspect.HostConfig?.Memory ?? 0;

  const cpu = history.map((p) => p.cpu);
  const mem = history.map((p) => p.memUsed);
  const rx = rateSeries(history, (i) => history[i].netRx);
  const tx = rateSeries(history, (i) => history[i].netTx);
  const rd = rateSeries(history, (i) => history[i].blkRead);
  const wr = rateSeries(history, (i) => history[i].blkWrite);
  const dash = '–';
  const off = !running;

  return (
    <div className="dk-c-charts">
      <StatChart
        tone="cpu"
        label={t('container.cpu')}
        value={last ? formatPercent(last.cpu) : dash}
        sub={ncpu ? t('container.ofCores', { n: ncpu }) : undefined}
        series={[{ values: cpu }]}
        max={Math.max(100, peak(cpu) * 1.15)}
        height={height}
      />
      <StatChart
        tone="mem"
        label={t('container.memory')}
        value={last ? formatBytes(last.memUsed) : dash}
        sub={last ? (limit > 0 ? t('container.memLimit', { limit: formatBytes(limit), pct: formatPercent(last.memPct, 0) }) : t('container.memNoLimit')) : undefined}
        series={[{ values: mem }]}
        max={limit > 0 ? limit : Math.max(1, peak(mem) * 1.2)}
        height={height}
      />
      <StatChart
        tone="net"
        label={t('container.network')}
        value={last && rx.length ? formatRate(rx[rx.length - 1] + tx[tx.length - 1]) : dash}
        sub={last && rx.length ? t('container.inOut', { rx: formatRate(rx[rx.length - 1]), tx: formatRate(tx[tx.length - 1]) }) : undefined}
        series={[{ values: rx, label: t('container.in'), color: 'var(--h-term)' }, { values: tx, label: t('container.out'), color: 'var(--h-log)' }]}
        max={Math.max(1024, peak(rx, tx) * 1.15)}
        height={height}
      />
      <StatChart
        tone="io"
        label={t('container.diskIo')}
        value={last && rd.length ? formatRate(rd[rd.length - 1] + wr[wr.length - 1]) : dash}
        sub={last && rd.length ? t('container.readWrite', { r: formatRate(rd[rd.length - 1]), w: formatRate(wr[wr.length - 1]) }) : undefined}
        series={[{ values: rd, label: t('container.read'), color: 'var(--h-file)' }, { values: wr, label: t('container.write'), color: 'var(--h-log)' }]}
        max={Math.max(1024, peak(rd, wr) * 1.15)}
        height={height}
      />
      {off && <p className="dk-muted dk-c-charts-off">{t('container.statsOff')}</p>}
    </div>
  );
}
