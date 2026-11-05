import { useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Icon } from '../../ui';
import {
  machineCpu,
  PROCESS_ROW_HEIGHT,
  processWindow,
  selectProcesses,
  type ProcessSort,
  type ProcessSortKey,
  type TaskProcess,
  type TaskSnapshot,
} from './model';

const percent = (value: number) =>
  `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })}%`;

export function Processes({
  snapshot,
  query,
  onInspect,
}: {
  snapshot: TaskSnapshot;
  query: string;
  onInspect(process: TaskProcess): void;
}) {
  const t = useT('taskmanager');
  const [sort, setSort] = useState<ProcessSort>({ key: 'cpu', direction: 'desc' });
  const [scroll, setScroll] = useState(0);
  const [height, setHeight] = useState(450);
  const root = useRef<HTMLDivElement>(null);
  const rows = useMemo(
    () => selectProcesses(snapshot.processes, query, sort),
    [snapshot.processes, query, sort],
  );
  useEffect(() => {
    if (!root.current) return;
    const observer = new ResizeObserver(([entry]) => setHeight(entry.contentRect.height));
    observer.observe(root.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (root.current) root.current.scrollTop = 0;
    setScroll(0);
  }, [query, sort]);
  const { start, end } = processWindow(rows.length, scroll, height);
  const heading = (key: ProcessSortKey, title: string) => (
    <th
      scope="col"
      title={key === 'cpu' ? t('hostCpuHint') : undefined}
      aria-sort={
        sort.key === key ? (sort.direction === 'asc' ? 'ascending' : 'descending') : 'none'
      }
    >
      <button
        type="button"
        onClick={() =>
          setSort({
            key,
            direction: sort.key === key && sort.direction === 'desc' ? 'asc' : 'desc',
          })
        }
      >
        {title}
        {sort.key === key && <Icon name={sort.direction === 'asc' ? 'chevronup' : 'chevron'} />}
      </button>
    </th>
  );
  return (
    <>
      <div
        className="tm-table-scroll"
        ref={root}
        tabIndex={0}
        aria-label={t('processes')}
        onScroll={(event) => {
          const top =
            Math.floor(event.currentTarget.scrollTop / PROCESS_ROW_HEIGHT) * PROCESS_ROW_HEIGHT;
          setScroll((current) => (current === top ? current : top));
        }}
      >
        <table className="tm-processes" aria-label={t('processes')} aria-rowcount={rows.length + 1}>
          <thead>
            <tr>
              {heading('name', t('column.name'))}
              {heading('pid', t('column.pid'))}
              {heading('user', t('column.user'))}
              <th scope="col">{t('column.status')}</th>
              {heading('cpu', t('cpu'))}
              {heading('memory', t('memory'))}
              <th scope="col" title={t('perProcessUnavailable')}>
                {t('gpu')}
              </th>
              <th scope="col" title={t('perProcessUnavailable')}>
                {t('network')}
              </th>
            </tr>
          </thead>
          <tbody>
            {start > 0 && (
              <tr aria-hidden="true">
                <td colSpan={8} style={{ height: start * PROCESS_ROW_HEIGHT, padding: 0 }} />
              </tr>
            )}
            {rows.slice(start, end).map((process, index) => {
              const cpu = machineCpu(process, snapshot.cpuCores);
              return (
                <tr key={process.pid} aria-rowindex={start + index + 2}>
                  <td>
                    <button
                      type="button"
                      className="tm-process-name"
                      onClick={() => onInspect(process)}
                      title={process.command || process.name}
                    >
                      <span>
                        <Icon name="terminal" />
                      </span>
                      <b>{process.name || t('unknown')}</b>
                    </button>
                  </td>
                  <td className="tm-muted tm-number">{process.pid}</td>
                  <td className="tm-user" title={process.user}>
                    {process.user || t('unavailableShort')}
                  </td>
                  <td className="tm-muted">
                    {t(`state.${process.state}`) === `state.${process.state}`
                      ? process.state
                      : t(`state.${process.state}`)}
                  </td>
                  <td
                    className="tm-usage"
                    style={
                      cpu === null
                        ? undefined
                        : {
                            backgroundColor: `color-mix(in srgb, var(--h-ov) ${Math.min(65, 4 + cpu * 1.8)}%, transparent)`,
                          }
                    }
                  >
                    {cpu === null ? (
                      <span title={t('protected')}>{t('unavailableShort')}</span>
                    ) : (
                      percent(cpu)
                    )}
                  </td>
                  <td
                    className="tm-usage"
                    style={
                      process.memoryAvailable
                        ? {
                            backgroundColor: `color-mix(in srgb, var(--h-ov) ${Math.min(65, 4 + process.memPercent * 1.8)}%, transparent)`,
                          }
                        : undefined
                    }
                  >
                    {process.memoryAvailable ? (
                      formatBytes(process.memory)
                    ) : (
                      <span title={t('protected')}>{t('unavailableShort')}</span>
                    )}
                  </td>
                  <td className="tm-muted" title={t('perProcessUnavailable')}>
                    —
                  </td>
                  <td className="tm-muted" title={t('perProcessUnavailable')}>
                    —
                  </td>
                </tr>
              );
            })}
            {end < rows.length && (
              <tr aria-hidden="true">
                <td
                  colSpan={8}
                  style={{ height: (rows.length - end) * PROCESS_ROW_HEIGHT, padding: 0 }}
                />
              </tr>
            )}
          </tbody>
        </table>
        {rows.length === 0 && (
          <div className="tm-empty">
            <Icon name="search" />
            <p>{t('noResults')}</p>
          </div>
        )}
      </div>
      <div className="tm-scope">
        <Icon name="info" />
        <span>
          {t('scope')} {t('perProcessUnavailable')}
        </span>
      </div>
    </>
  );
}
