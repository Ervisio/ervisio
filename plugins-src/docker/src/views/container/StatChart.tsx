import type { ReactNode } from 'react';
import { AreaChart, type Series } from '../../kit';

/** One live chart tile: label, current value, a sub line and the area chart. */
export function StatChart({ label, value, sub, series, max, height = 70, tone }: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  series: Series[];
  max?: number;
  height?: number;
  tone?: 'cpu' | 'mem' | 'net' | 'io';
}) {
  return (
    <div className={`dk-c-st dk-c-st--${tone ?? 'cpu'}`}>
      <small>{label}</small>
      <div className="dk-c-st-v"><b>{value}</b>{sub && <span className="dk-muted">{sub}</span>}</div>
      <AreaChart series={series} height={height} max={max} min={0} label={label} />
    </div>
  );
}

/** Per-second rate of a cumulative counter between neighbouring points. */
export function rateSeries(points: { t: number }[], get: (i: number) => number): number[] {
  const out: number[] = [];
  for (let i = 1; i < points.length; i++) {
    const dt = (points[i].t - points[i - 1].t) / 1000;
    out.push(dt > 0 ? Math.max(0, (get(i) - get(i - 1)) / dt) : 0);
  }
  return out;
}

export const peak = (...lists: number[][]): number => Math.max(0, ...lists.flatMap((l) => l));
