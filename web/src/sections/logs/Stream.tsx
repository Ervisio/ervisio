import { memo, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Badge, Button, EmptyState, Skeleton } from '../../ui';
import { useI18n, useT } from '../../i18n';
import { fmtClock, fmtDay, hueFor } from './helpers';
import type { Entry } from './types';

interface Props {
  rows: Entry[];
  selectedKey: string | null;
  onSelect(e: Entry | null): void;
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  onMore(): void;
  error?: string;
  onRetry(): void;
  onClear(): void;
  withDate: boolean;
  mobile: boolean;
  live: boolean;
}

const ROW = 32;
const ROW_MOBILE = 64;
const OVERSCAN = 10;

interface RowProps {
  e: Entry;
  sel: boolean;
  withDate: boolean;
  mobile: boolean;
  lang: string;
  levelLabel: string;
  onPick(e: Entry): void;
}

const Row = memo(function Row({ e, sel, withDate, mobile, lang, levelLabel, onPick }: RowProps) {
  const time = withDate ? `${fmtDay(e.ts, lang)} ${fmtClock(e.ts, false)}` : fmtClock(e.ts);
  return (
    <div
      role="option"
      aria-selected={sel}
      className={`logs-row lv-${e.level}${sel ? ' sel' : ''}`}
      style={{ height: mobile ? ROW_MOBILE : ROW }}
      onClick={() => onPick(e)}
    >
      <span className="tm">{time}</span>
      <span className={`lvb lv-${e.level}`}>{levelLabel}</span>
      <span className={`src logs-h-${hueFor(e.source)}`} title={e.source}>{e.source}</span>
      <span className="msg" title={e.message}>{e.message}</span>
    </div>
  );
});

export function Stream({ rows, selectedKey, onSelect, loading, loadingMore, hasMore, onMore, error, onRetry, onClear, withDate, mobile, live }: Props) {
  const t = useT('logs');
  const { lang } = useI18n();
  const rowH = mobile ? ROW_MOBILE : ROW;
  const ref = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(0);
  const [height, setHeight] = useState(600);
  const [unseen, setUnseen] = useState(0);
  const prev = useRef<{ first?: string; n: number }>({ n: 0 });
  const raf = useRef(0);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setHeight(el.clientHeight));
    ro.observe(el);
    setHeight(el.clientHeight);
    return () => ro.disconnect();
  }, []);

  const onScroll = useCallback(() => {
    if (raf.current) return;
    raf.current = requestAnimationFrame(() => {
      raf.current = 0;
      const el = ref.current;
      if (!el) return;
      setTop(el.scrollTop);
      if (el.scrollTop < rowH) setUnseen(0);
    });
  }, [rowH]);

  // Keep the view steady when rows are added on top (live), reset on a new query.
  useLayoutEffect(() => {
    const el = ref.current;
    const p = prev.current;
    const first = rows[0]?.key;
    if (el && p.first && first && first !== p.first) {
      let k = -1;
      const lim = Math.min(rows.length, 5000);
      for (let i = 0; i < lim; i++) if (rows[i].key === p.first) { k = i; break; }
      if (k > 0) {
        if (el.scrollTop > rowH) {
          el.scrollTop += k * rowH;
          setUnseen((u) => u + k);
        }
      } else if (k < 0) {
        el.scrollTop = 0;
        setUnseen(0);
      }
    } else if (el && !first) {
      el.scrollTop = 0;
    }
    prev.current = { first, n: rows.length };
  }, [rows, rowH]);

  // fill the viewport / load more near the end
  const nearEnd = top + height > rows.length * rowH - 900;
  useEffect(() => {
    if (hasMore && !loadingMore && !loading && !error && rows.length > 0 && nearEnd) onMore();
  }, [hasMore, loadingMore, loading, error, rows.length, nearEnd, onMore]);

  const pick = useCallback((e: Entry) => onSelect(e), [onSelect]);

  const onKey = (ev: React.KeyboardEvent) => {
    if (!rows.length) return;
    const i = rows.findIndex((r) => r.key === selectedKey);
    let n: number;
    if (ev.key === 'ArrowDown' || ev.key === 'j') n = Math.min(rows.length - 1, i + 1);
    else if (ev.key === 'ArrowUp' || ev.key === 'k') n = i <= 0 ? 0 : i - 1;
    else if (ev.key === 'Home') n = 0;
    else if (ev.key === 'End') n = rows.length - 1;
    else if (ev.key === 'Escape') return onSelect(null);
    else return;
    ev.preventDefault();
    onSelect(rows[n]);
    const el = ref.current;
    if (el) {
      if (n * rowH < el.scrollTop) el.scrollTop = n * rowH;
      else if ((n + 1) * rowH > el.scrollTop + el.clientHeight) el.scrollTop = (n + 1) * rowH - el.clientHeight;
    }
  };

  const start = Math.max(0, Math.floor(top / rowH) - OVERSCAN);
  const end = Math.min(rows.length, Math.ceil((top + height) / rowH) + OVERSCAN);
  const visible = rows.slice(start, end);
  const lv = (l: string) => t(`level.${l}`);

  let body;
  if (error && !rows.length) {
    body = <EmptyState icon="alert" title={t('stream.loadError')} text={error} action={<Button onClick={onRetry}>{t('stream.retry')}</Button>} />;
  } else if (loading && !rows.length) {
    body = (
      <div className="logs-skel" aria-busy="true">
        <Skeleton lines={12} height={16} />
      </div>
    );
  } else if (!rows.length) {
    body = <EmptyState icon="logs" hue="log" title={t('stream.empty')} text={t('stream.emptyText')} action={<Button onClick={onClear}>{t('stream.clear')}</Button>} />;
  }

  return (
    <div className="logs-streamwrap">
      {live && unseen > 0 && (
        <button type="button" className="logs-new" onClick={() => { if (ref.current) ref.current.scrollTop = 0; setUnseen(0); }}>
          <Badge tone="ok">{t('live.paused', { count: unseen })}</Badge>
        </button>
      )}
      <div
        ref={ref}
        className="logs-stream"
        tabIndex={0}
        role="listbox"
        aria-label={t('stream.label')}
        aria-busy={loading}
        onScroll={onScroll}
        onKeyDown={onKey}
      >
        {body ? (
          body
        ) : (
          <div style={{ height: rows.length * rowH + 48, position: 'relative' }}>
            <div style={{ transform: `translateY(${start * rowH}px)` }}>
              {visible.map((e) => (
                <Row key={e.key} e={e} sel={e.key === selectedKey} withDate={withDate} mobile={mobile} lang={lang} levelLabel={lv(e.level)} onPick={pick} />
              ))}
            </div>
            <div className="logs-foot" style={{ top: rows.length * rowH }}>
              {loadingMore ? t('stream.loadingMore') : hasMore ? '' : error ? <Button size="sm" onClick={onRetry}>{t('stream.retry')}</Button> : t('stream.end')}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
