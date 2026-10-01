import { useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '../../i18n';
import { Icon, type IconName } from '../../ui';

export interface PaletteItem {
  id: string;
  group: 'snippets' | 'sessions' | 'history' | 'actions';
  title: string;
  sub?: string;
  icon: IconName;
  hue: string;
  keywords?: string;
  run(): void;
  /** Shift+Enter: insert without running. */
  paste?(): void;
}

const ORDER: PaletteItem['group'][] = ['snippets', 'sessions', 'history', 'actions'];

export default function Palette({ items, onClose }: { items: PaletteItem[]; onClose(): void }) {
  const t = useT('terminal');
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);

  useEffect(() => input.current?.focus(), []);

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const words = needle.split(/\s+/).filter(Boolean);
    const match = (it: PaletteItem) => {
      const hay = `${it.title} ${it.sub ?? ''} ${it.keywords ?? ''}`.toLowerCase();
      return words.every((w) => hay.includes(w));
    };
    const out: PaletteItem[] = [];
    for (const g of ORDER) {
      let n = 0;
      for (const it of items) if (it.group === g && match(it) && n++ < (needle ? 8 : 6)) out.push(it);
    }
    return out;
  }, [items, q]);

  useEffect(() => setSel(0), [q]);
  useEffect(() => {
    list.current?.querySelector<HTMLElement>('[data-sel="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [sel, shown]);

  const choose = (it: PaletteItem | undefined, paste: boolean) => {
    if (!it) return;
    onClose();
    if (paste && it.paste) it.paste();
    else it.run();
  };

  return (
    <div className="terminal-palscrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="terminal-pal" role="dialog" aria-label={t('palette.title')}>
        <div className="terminal-pal-in">
          <Icon name="search" />
          <input
            ref={input}
            value={q}
            placeholder={t('palette.placeholder')}
            aria-label={t('palette.placeholder')}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                e.preventDefault();
                onClose();
              } else if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSel((s) => Math.min(shown.length - 1, s + 1));
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSel((s) => Math.max(0, s - 1));
              } else if (e.key === 'Enter') {
                e.preventDefault();
                choose(shown[sel], e.shiftKey);
              }
            }}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="terminal-pal-list" ref={list}>
          {shown.length === 0 && <div className="terminal-pal-empty">{t('palette.empty')}</div>}
          {ORDER.map((g) => {
            const rows = shown.filter((i) => i.group === g);
            if (!rows.length) return null;
            return (
              <div key={g}>
                <div className="terminal-pal-grp">{t(`palette.${g}`)}</div>
                {rows.map((it) => {
                  const idx = shown.indexOf(it);
                  return (
                    <div key={it.id} className={`terminal-snip${idx === sel ? ' on' : ''}`} data-sel={idx === sel} role="option" aria-selected={idx === sel} onMouseMove={() => setSel(idx)} onClick={() => choose(it, false)}>
                      <span className="terminal-ic" style={{ ['--h' as string]: `var(--h-${it.hue})`, ['--s' as string]: `var(--h-${it.hue}-s)` }}>
                        <Icon name={it.icon} />
                      </span>
                      <div>
                        <b>{it.title}</b>
                        {it.sub && <code>{it.sub}</code>}
                      </div>
                      {idx === sel && g !== 'sessions' && g !== 'actions' && <span className="terminal-run"><Icon name="play" /></span>}
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
