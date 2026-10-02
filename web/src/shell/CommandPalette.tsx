import { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { useT } from '../i18n';
import { Icon, useFocusTrap, type IconName } from '../ui';
import { useAllPaletteActions } from '../sections';
import { NavIcon } from './NavIcon';
import { useNav } from './useNav';

interface Row {
  id: string;
  group: 'go' | 'act';
  title: string;
  hint?: string;
  icon: IconName;
  logo?: string;
  hue?: string;
  hay: string;
  run(): void;
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose(): void }) {
  const t = useT('shell');
  const nav = useNavigate();
  const { main, plugins, settings } = useNav();
  const extra = useAllPaletteActions();
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  useFocusTrap(ref, open, onClose);
  useEffect(() => {
    if (open) {
      setQ('');
      setSel(0);
    }
  }, [open]);

  const rows = useMemo<Row[]>(() => {
    const go: Row[] = [...main, ...plugins, settings].map((e) => ({
      id: `go:${e.key}`,
      group: 'go',
      title: t('palette.goTo', { name: e.label }),
      icon: e.icon,
      logo: e.logo,
      hue: e.hueClass,
      hay: e.label.toLowerCase(),
      run: () => nav(e.to),
    }));
    const act: Row[] = extra.map((a) => ({
      id: `act:${a.id}`,
      group: 'act',
      title: a.title,
      hint: a.hint,
      icon: a.icon ?? 'play',
      hue: a.hue ? `hue-${a.hue}` : undefined,
      hay: `${a.title} ${a.hint ?? ''} ${(a.keywords ?? []).join(' ')}`.toLowerCase(),
      run: a.run,
    }));
    return [...go, ...act];
  }, [main, plugins, settings, extra, nav, t]);

  const shown = useMemo(() => {
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    return words.length ? rows.filter((r) => words.every((w) => r.hay.includes(w))) : rows;
  }, [rows, q]);

  useEffect(() => setSel(0), [q]);
  useEffect(() => {
    listRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [sel, shown]);

  if (!open) return null;
  const run = (r?: Row) => {
    if (!r) return;
    onClose();
    r.run();
  };

  return createPortal(
    <div className="pal-scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={ref} className="pal" role="dialog" aria-modal="true" aria-label={t('palette.title')}>
        <div className="pal-in">
          <Icon name="search" />
          <input
            data-autofocus
            role="combobox"
            aria-expanded="true"
            aria-controls="pal-list"
            placeholder={t('palette.placeholder')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSel((s) => Math.min(shown.length - 1, s + 1));
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSel((s) => Math.max(0, s - 1));
              } else if (e.key === 'Enter') {
                e.preventDefault();
                run(shown[sel]);
              }
            }}
          />
          <kbd className="ui-kbd">Esc</kbd>
        </div>
        <div className="pal-list" id="pal-list" role="listbox" ref={listRef}>
          {shown.length === 0 && <div className="pal-empty">{t('palette.empty')}</div>}
          {shown.map((r, i) => (
            <div key={r.id}>
              {(i === 0 || shown[i - 1].group !== r.group) && <div className="pal-g">{r.group === 'go' ? t('palette.sections') : t('palette.actions')}</div>}
              <button type="button" role="option" aria-selected={i === sel} className={`pal-i ${r.hue ?? ''}`} onMouseMove={() => setSel(i)} onClick={() => run(r)}>
                <span className="ic"><NavIcon e={r} /></span>
                {r.title}
                {r.hint && <small>{r.hint}</small>}
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>,
    document.body,
  );
}
