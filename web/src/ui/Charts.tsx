export interface Series {
  values: number[];
  color?: string;
  label?: string;
}

function path(values: number[], min: number, max: number, w: number, h: number, pad: number) {
  const n = values.length;
  const rng = max - min || 1;
  const pts = values.map((v, i) => [n === 1 ? w / 2 : (i / (n - 1)) * w, pad + (1 - (v - min) / rng) * (h - pad * 2)] as const);
  return pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)},${y.toFixed(2)}`).join(' ');
}

/** Tiny line chart without axes. Colour defaults to the section colour (--h) or the accent. */
export function Sparkline({ values, height = 32, color = 'var(--h, var(--acc))', fill = true, min, max }: { values: number[]; height?: number; color?: string; fill?: boolean; min?: number; max?: number }) {
  const w = 100;
  if (values.length < 2) return <svg className="ui-spark" style={{ height }} aria-hidden="true" />;
  const lo = min ?? Math.min(...values);
  const hi = max ?? Math.max(...values);
  const d = path(values, lo, hi, w, height, 3);
  return (
    <svg className="ui-spark" style={{ height }} viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" aria-hidden="true">
      {fill && <path d={`${d} L${w},${height} L0,${height} Z`} fill={color} opacity={0.15} />}
      <path className="line" d={d} stroke={color} vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

/** Multi-series area chart (e.g. CPU / network over time). Fixed 0..max scale. */
export function AreaChart({ series, height = 150, max, min = 0, label }: { series: Series[]; height?: number; max?: number; min?: number; label?: string }) {
  const w = 100;
  const hi = max ?? Math.max(1, ...series.flatMap((s) => s.values));
  const palette = ['var(--h-ov)', 'var(--h-file)', 'var(--h-term)', 'var(--h-log)'];
  return (
    <div>
      <svg className="ui-chart" style={{ height }} viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" role="img" aria-label={label}>
        {series.map((s, i) => {
          if (s.values.length < 2) return null;
          const c = s.color ?? palette[i % palette.length];
          const d = path(s.values, min, hi, w, height, 3);
          return (
            <g key={i}>
              {i === 0 && <path className="area" d={`${d} L${w},${height} L0,${height} Z`} fill={c} />}
              <path className="line" d={d} stroke={c} vectorEffect="non-scaling-stroke" />
            </g>
          );
        })}
      </svg>
      {series.some((s) => s.label) && (
        <div className="ui-chart-leg">
          {series.map((s, i) => s.label && <span key={i}><i style={{ background: s.color ?? ['var(--h-ov)', 'var(--h-file)', 'var(--h-term)', 'var(--h-log)'][i % 4] }} />{s.label}</span>)}
        </div>
      )}
    </div>
  );
}
