import { useEffect, useState } from 'react';
import { docker, errorText } from '../../api/engine';
import { formatBytes, formatPercent } from '../../api/format';
import { useStats } from '../../api/hooks';
import type { ContainerInspect } from '../../api/types';
import { t } from '../../i18n';
import { EmptyState } from '../../kit';
import { LiveCharts } from './LiveCharts';

interface Top {
  Titles: string[];
  Processes: string[][];
}

/** Bigger charts, the numbers behind them and the process list of the container. */
export function StatsTab({ id, inspect, running }: { id: string; inspect: ContainerInspect; running: boolean }) {
  const { last } = useStats(id, running, 1000);
  const [top, setTop] = useState<Top | null>(null);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!running) {
      setTop(null);
      return;
    }
    let live = true;
    let plain = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = async () => {
      try {
        const r = await docker.get<Top>(`/containers/${encodeURIComponent(id)}/top`, plain ? undefined : { ps_args: 'aux' });
        if (!live) return;
        setTop(r);
        setErr('');
      } catch (e) {
        if (!plain) plain = true; // busybox images have no "ps aux": use the Engine's default columns
        else if (live) setErr(errorText(e));
      }
      if (live) timer = setTimeout(load, 3000);
    };
    void load();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [id, running]);

  const dash = '–';
  const mem = inspect.HostConfig?.Memory ?? 0;
  const cells: [string, string][] = [
    [t('container.cpu'), last ? formatPercent(last.cpu) : dash],
    [t('container.memory'), last ? `${formatBytes(last.memUsed)}${mem > 0 ? ` / ${formatBytes(mem)}` : ''}` : dash],
    [t('container.pids'), last ? String(last.pids) : dash],
    [t('container.netRx'), last ? formatBytes(last.netRx) : dash],
    [t('container.netTx'), last ? formatBytes(last.netTx) : dash],
    [t('container.blkRead'), last ? formatBytes(last.blkRead) : dash],
    [t('container.blkWrite'), last ? formatBytes(last.blkWrite) : dash],
  ];
  const cmdCol = top ? top.Titles.length - 1 : -1;

  return (
    <div className="dk-c-ov">
      <LiveCharts id={id} running={running} inspect={inspect} height={150} />
      <div className="dk-card">
        <h3>{t('container.totals')}</h3>
        <div className="dk-c-totals">
          {cells.map(([k, v]) => (
            <div key={k}><small>{k}</small><b>{v}</b></div>
          ))}
        </div>
      </div>
      <div className="dk-card">
        <h3>{t('container.processes')}{top && <span className="dk-muted dk-c-h3-n">{top.Processes.length}</span>}</h3>
        {!running ? (
          <EmptyState icon="cpu" hue="file" title={t('container.statsStopped')} />
        ) : err ? (
          <p className="dk-muted">{err}</p>
        ) : !top ? (
          <p className="dk-muted">{t('common.loading')}</p>
        ) : (
          <div className="dk-tablewrap">
            <table className="dk-table dk-c-proc">
              <thead><tr>{top.Titles.map((h, i) => <th key={h + i} className={i === cmdCol ? '' : 'dk-num'}>{h}</th>)}</tr></thead>
              <tbody>
                {top.Processes.map((p, i) => (
                  <tr key={i}>{p.map((c, j) => <td key={j} className={j === cmdCol ? 'dk-mono dk-c-cmd' : 'dk-num'}>{c}</td>)}</tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
