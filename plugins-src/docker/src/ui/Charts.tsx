import { Sparkline } from '../kit';

export { Sparkline, AreaChart } from '../kit';
export type { Series } from '../kit';

/** Colour class for a usage percentage: green, amber from 70, red from 90. */
export const usageTone = (pct: number): 'ok' | 'warn' | 'err' => (pct >= 90 ? 'err' : pct >= 70 ? 'warn' : 'ok');

/** Thin usage bar. `value` and `max` use the same unit; the bar is capped at 100%. */
export function MiniMeter({ value, max = 100, label }: { value: number; max?: number; label?: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className={`dk-meter dk-meter--${usageTone(pct)}`} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={max} aria-valuenow={Math.round(value)}>
      <i style={{ width: `${pct}%` }} />
    </span>
  );
}

/** CPU or memory history as a small filled line. Shows an empty slot until there are two points. */
export function MiniSpark({ values, max }: { values: number[]; max?: number }) {
  return <div className="dk-spark"><Sparkline values={values} height={28} min={0} max={max} color="var(--acc)" /></div>;
}
