import { useEffect, useState } from 'react';
import { Button, Dialog, DropdownMenu, IconButton, Input, type MenuItem } from '../../ui';
import { useI18n, useT } from '../../i18n';
import { fmtClock, fmtDay, toLocalInput, type Filter, type RangeId } from './helpers';
import { LEVELS, type Level } from './types';

interface Props {
  filter: Filter;
  counts: Record<Level, number> | null;
  onChange(patch: Partial<Filter>): void;
  onRefresh(): void;
  onSources(): void;
  sourcesLabel: string;
  showSourcesButton: boolean;
}

const RANGE_IDS: Exclude<RangeId, 'custom'>[] = ['15m', '1h', '24h', '7d'];

function num(n: number): string {
  return n >= 100000 ? `${Math.round(n / 1000)}k` : n.toLocaleString();
}

export function rangeLabel(t: (k: string, v?: Record<string, string | number>) => string, f: Filter, lang: string): string {
  if (f.range !== 'custom') return t(`range.short${f.range}`);
  const a = f.from ?? 0;
  const b = f.to ?? Date.now();
  const secs = b - a < 10 * 60_000;
  const c = (x: number) => fmtClock(x, secs);
  const today = new Date().toDateString();
  const sameDay = new Date(a).toDateString() === new Date(b).toDateString();
  if (sameDay) return `${new Date(a).toDateString() === today ? '' : fmtDay(a, lang) + ', '}${c(a)} – ${c(b)}`;
  return `${fmtDay(a, lang)} ${c(a)} – ${fmtDay(b, lang)} ${c(b)}`;
}

export function FilterBar({ filter, counts, onChange, onRefresh, onSources, sourcesLabel, showSourcesButton }: Props) {
  const t = useT('logs');
  const { lang } = useI18n();
  const [text, setText] = useState(filter.q);
  const [custom, setCustom] = useState(false);

  // keep the box in step with the URL (links, clear filters) without fighting typing
  useEffect(() => setText(filter.q), [filter.q]);
  useEffect(() => {
    if (text === filter.q) return;
    const id = window.setTimeout(() => onChange({ q: text }), 300);
    return () => window.clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text]);

  const toggle = (l: Level) => {
    const on = filter.levels.includes(l);
    const next = on ? filter.levels.filter((x) => x !== l) : LEVELS.filter((x) => x === l || filter.levels.includes(x));
    if (next.length) onChange({ levels: next });
  };

  const items: MenuItem[] = [
    ...RANGE_IDS.map((id) => ({ id, label: t(`range.${id}`), icon: (filter.range === id ? 'check' : undefined) as 'check' | undefined, onSelect: () => onChange({ range: id, from: undefined, to: undefined }) })),
    { type: 'separator' as const },
    { id: 'custom', label: t('range.custom'), icon: (filter.range === 'custom' ? 'check' : 'clock') as 'check' | 'clock', onSelect: () => setCustom(true) },
  ];

  return (
    <div className="logs-fbar">
      {showSourcesButton && (
        <Button icon="filter" onClick={onSources} aria-label={t('sources.title')}>
          <span className="logs-clip">{sourcesLabel}</span>
        </Button>
      )}
      <div className="logs-fsearch">
        <Input
          compact
          mono
          icon="search"
          type="search"
          aria-label={t('searchLabel')}
          placeholder={t('searchPlaceholder')}
          value={text}
          onChange={(e) => setText(e.target.value)}
          spellCheck={false}
          autoComplete="off"
        />
      </div>
      <div className="logs-lvls" role="group" aria-label={t('level.group')}>
        {LEVELS.map((l) => {
          const on = filter.levels.includes(l);
          return (
            <button
              key={l}
              type="button"
              className={`logs-lvl lv-${l}`}
              aria-pressed={on}
              title={t('level.toggle', { level: t(`level.${l}`), state: t(on ? 'level.shown' : 'level.hidden') })}
              onClick={() => toggle(l)}
            >
              {t(`level.${l}`)}
              {counts && <b>{num(counts[l])}</b>}
            </button>
          );
        })}
      </div>
      <DropdownMenu
        aria-label={t('range.label')}
        items={items}
        trigger={(p) => (
          <Button {...p} icon="clock" aria-label={`${t('range.label')}: ${rangeLabel(t, filter, lang)}`}>
            {rangeLabel(t, filter, lang)}
          </Button>
        )}
      />
      <button type="button" className={`logs-live${filter.live ? ' on' : ''}`} aria-pressed={filter.live} title={t('live.label')} onClick={() => onChange({ live: !filter.live })}>
        <i />
        {t('live.on')}
      </button>
      <IconButton icon="refresh" label={t('refresh')} onClick={onRefresh} />
      {custom && <CustomRange filter={filter} onClose={() => setCustom(false)} onApply={(from, to) => { onChange({ range: 'custom', from, to, live: false }); setCustom(false); }} />}
    </div>
  );
}

function CustomRange({ filter, onClose, onApply }: { filter: Filter; onClose(): void; onApply(from: number, to: number): void }) {
  const t = useT('logs');
  const now = Date.now();
  const [from, setFrom] = useState(toLocalInput(filter.from ?? now - 3_600_000));
  const [to, setTo] = useState(toLocalInput(filter.to ?? now));
  const a = new Date(from).getTime();
  const b = new Date(to).getTime();
  const bad = !(Number.isFinite(a) && Number.isFinite(b) && b > a);
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('range.customTitle')}
      icon="clock"
      onSubmit={() => !bad && onApply(a, b)}
      footer={
        <>
          <Button type="button" variant="ghost" onClick={onClose}>{t('watch.cancel')}</Button>
          <Button type="submit" variant="primary" disabled={bad}>{t('range.apply')}</Button>
        </>
      }
    >
      <div className="logs-custom">
        <Input label={t('range.from')} type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} />
        <Input label={t('range.to')} type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} error={bad ? t('range.invalid') : undefined} />
      </div>
    </Dialog>
  );
}

