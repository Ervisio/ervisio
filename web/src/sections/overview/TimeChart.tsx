import { useMemo } from 'react';
import type { Point } from './data';

export interface TimeSeries {
  color: string;
  /** Value per history point, already scaled to 0..100. */
  get(p: Point): number;
  /** Fill under the line. */
  fill?: boolean;
}

/**
 * Line chart on a real time axis: the newest sample is at the right edge and the left part stays
 * empty until enough history has been collected.
 */
export function TimeChart({ points, windowMs, series, height = 150, label }: { points: Point[]; windowMs: number; series: TimeSeries[]; height?: number; label: string }) {
  const W = 100;
  const paths = useMemo(() => {
    const now = points.length ? points[points.length - 1].t : Date.now();
    const from = now - windowMs;
    const vis = points.filter((p) => p.t >= from - 10_000);
    return series.map((s) => {
      const pts = vis.map((p) => {
        const x = Math.max(0, ((p.t - from) / windowMs) * W);
        const v = Math.max(0, Math.min(100, s.get(p)));
        return [x, 4 + (1 - v / 100) * (height - 8)] as const;
      });
      if (pts.length < 2) return { d: '', area: '', color: s.color };
      const d = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)},${y.toFixed(2)}`).join(' ');
      const area = s.fill ? `${d} L${pts[pts.length - 1][0].toFixed(2)},${height} L${pts[0][0].toFixed(2)},${height} Z` : '';
      return { d, area, color: s.color };
    });
  }, [points, windowMs, series, height]);
  return (
    <svg className="ov-chart" style={{ height }} viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" role="img" aria-label={label}>
      {paths.map((p, i) => p.d && (
        <g key={i}>
          {p.area && <path d={p.area} fill={p.color} opacity={0.15} />}
          <path d={p.d} className="ov-chart-line" stroke={p.color} vectorEffect="non-scaling-stroke" />
        </g>
      ))}
    </svg>
  );
}
