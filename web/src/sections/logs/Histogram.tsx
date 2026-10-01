import { useRef, useState } from 'react';
import { useI18n, useT } from '../../i18n';
import { fmtAxis, fmtClock, fmtDay } from './helpers';
import type { HistogramResult, Level } from './types';

interface Props {
  data: HistogramResult | null;
  levels: Level[];
  onZoom(from: number, to: number): void;
  loading: boolean;
}

export function Histogram({ data, levels, onZoom, loading }: Props) {
  const t = useT('logs');
  const { lang } = useI18n();
  const [focus, setFocus] = useState(0);
  const box = useRef<HTMLDivElement>(null);
  const show = (l: Level) => levels.includes(l);

  const buckets = data?.buckets ?? [];
  const totals = buckets.map((b) => (show('err') ? b.err : 0) + (show('warn') ? b.warn : 0) + (show('info') ? b.info : 0) + (show('debug') ? b.debug : 0));
  const max = Math.max(1, ...totals);
  const span = data ? data.until - data.since : 0;

  const label = (i: number) => {
    const b = buckets[i];
    const time = span > 36 * 3_600_000 ? `${fmtDay(b.t, lang)} ${fmtClock(b.t, false)}` : fmtClock(b.t, false);
    return t('histogram.bar', { time, err: b.err, warn: b.warn, info: b.info, debug: b.debug });
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (!buckets.length) return;
    let n: number;
    if (e.key === 'ArrowRight') n = Math.min(buckets.length - 1, focus + 1);
    else if (e.key === 'ArrowLeft') n = Math.max(0, focus - 1);
    else return;
    e.preventDefault();
    setFocus(n);
    (box.current?.children[n] as HTMLElement | undefined)?.focus();
  };

  const ticks = data ? [0, 0.25, 0.5, 0.75, 1].map((f) => data.since + f * span) : [];

  return (
    <div className={`logs-hist${loading ? ' loading' : ''}`}>
      <div className="logs-bars" ref={box} role="group" aria-label={t('histogram.label')} onKeyDown={onKey}>
        {buckets.map((b, i) => {
          const h = (n: number) => `${(n / max) * 100}%`;
          return (
            <button
              key={b.t}
              type="button"
              className="logs-bar"
              tabIndex={i === focus ? 0 : -1}
              aria-label={label(i)}
              title={`${label(i)}\n${t('histogram.zoom')}`}
              onFocus={() => setFocus(i)}
              onClick={() => onZoom(b.t, Math.min(b.t + (data?.step ?? 60000), data?.until ?? b.t + 60000))}
            >
              {show('debug') && b.debug > 0 && <span className="d" style={{ height: h(b.debug) }} />}
              {show('info') && b.info > 0 && <span className="i" style={{ height: h(b.info) }} />}
              {show('warn') && b.warn > 0 && <span className="w" style={{ height: h(b.warn) }} />}
              {show('err') && b.err > 0 && <span className="e" style={{ height: h(b.err) }} />}
            </button>
          );
        })}
        {!buckets.length && <div className="logs-bars-empty" />}
      </div>
      <div className="logs-htime" aria-hidden="true">
        {ticks.map((tk, i) => (
          <span key={i}>{fmtAxis(tk, span, lang)}</span>
        ))}
      </div>
    </div>
  );
}
