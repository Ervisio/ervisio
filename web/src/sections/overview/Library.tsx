import { useMemo, useRef, useState, type PointerEvent } from 'react';
import { useT } from '../../i18n';
import { Button, Icon, Input, hueClass, type HueId, type IconName } from '../../ui';
import { usePlugins } from '../../plugins';
import { useSession } from '../../api';
import { CATALOG, type Cols } from './model';

export interface LibItem {
  key: string;
  type: string;
  category: 'basics' | 'system' | 'personal' | 'plugins';
  icon: IconName;
  hue: HueId;
  title: string;
  desc: string;
  cols: Cols;
  tag?: string;
  configure?: boolean;
  settings?(): Record<string, any> | undefined;
}

export function useLibraryItems(): LibItem[] {
  const t = useT('overview');
  const win = useSession().session?.os === 'windows';
  const { widgets, plugins } = usePlugins();
  return useMemo(() => {
    const own: LibItem[] = CATALOG.map((c) => ({
      key: `${c.type}:${c.category}`,
      type: c.type,
      category: c.category,
      icon: c.icon,
      hue: c.hue,
      title: t(`library.${c.type}.title`),
      desc: t(win && c.type === 'service' ? 'win.library.service.desc' : `library.${c.type}.desc`),
      cols: c.cols,
      configure: c.configure,
      settings: c.settings,
    }));
    const plug: LibItem[] = widgets.map((w) => {
      const m = plugins.find((p) => p.id === w.plugin);
      return {
        key: `plugin:${w.plugin}/${w.id}`,
        type: 'plugin',
        category: 'plugins',
        icon: (w.icon as IconName) ?? 'plugins',
        hue: 'plg',
        title: w.title,
        desc: t('library.pluginDesc', { plugin: m?.name ?? w.plugin }),
        cols: ([3, 4, 5, 6, 7, 12].includes(w.cols ?? 0) ? w.cols : 6) as Cols,
        tag: t('library.pluginTag'),
        settings: () => ({ plugin: w.plugin, widget: w.id }),
      };
    });
    return [...own, ...plug];
  }, [t, widgets, plugins, win]);
}

export function Library({ onAdd, onPointerDown, onReset, className }: { onAdd(i: LibItem): void; onPointerDown(i: LibItem, e: PointerEvent<HTMLElement>): void; onReset(): void; className?: string }) {
  const t = useT('overview');
  const items = useLibraryItems();
  const [q, setQ] = useState('');
  const ptype = useRef('mouse');
  const needle = q.trim().toLowerCase();
  const shown = items.filter((i) => !needle || `${i.title} ${i.desc}`.toLowerCase().includes(needle));
  const cats = (['basics', 'system', 'personal', 'plugins'] as const).map((c) => ({ id: c, list: shown.filter((i) => i.category === c) })).filter((c) => c.list.length);
  return (
    <aside className={`ov-lib${className ? ' ' + className : ''}`} aria-label={t('library.title')}>
      <h2>{t('library.title')}</h2>
      <p>{t('library.hint')}</p>
      <Input icon="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('library.search')} aria-label={t('library.search')} compact />
      {cats.map((c) => (
        <div key={c.id}>
          <div className="ov-cat">{t(`library.cat.${c.id}`)}</div>
          {c.list.map((i) => (
            <button
              key={i.key}
              type="button"
              className={`ov-item ${hueClass(i.hue)}`}
              onPointerDown={(e) => {
                ptype.current = e.pointerType;
                onPointerDown(i, e);
              }}
              onClick={(e) => {
                if (e.detail === 0 || ptype.current === 'touch') onAdd(i); // keyboard and touch; a mouse click is handled by the pointer logic
              }}
            >
              <span className="ov-ic"><Icon name={i.icon} /></span>
              <span className="ov-item-tx"><b>{i.title}</b><small>{i.desc}</small></span>
              {i.tag && <span className="ov-tag">{i.tag}</span>}
            </button>
          ))}
        </div>
      ))}
      {!cats.length && <p className="ov-lib-none">{t('library.noMatch')}</p>}
      <div className="ov-lib-foot">
        <Button variant="ghost" icon="undo" onClick={onReset}>{t('library.reset')}</Button>
      </div>
    </aside>
  );
}
