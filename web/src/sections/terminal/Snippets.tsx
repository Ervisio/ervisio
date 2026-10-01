import { useEffect, useRef } from 'react';
import { useT } from '../../i18n';
import { Icon, useContextMenu, type IconName } from '../../ui';
import type { Snippet } from './types';

const HUES = ['sw', 'term', 'log', 'plg', 'file', 'svc', 'usr', 'ov'] as const;
const ICONS: IconName[] = ['download', 'refresh', 'logs', 'broom', 'server', 'disk', 'command', 'cpu'];

export function snippetLook(s: Snippet) {
  let n = 0;
  for (const c of s.title) n = (n * 31 + c.charCodeAt(0)) >>> 0;
  return { hue: HUES[n % HUES.length], icon: ICONS[n % ICONS.length] };
}

function Chip({ s, onPaste, onRun, onEdit, onDelete }: { s: Snippet; onPaste(): void; onRun(): void; onEdit(): void; onDelete(): void }) {
  const t = useT('terminal');
  const timer = useRef<number>();
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const look = snippetLook(s);
  const cm = useContextMenu(() => [
    { id: 'run', label: t('snippet.run'), icon: 'play', onSelect: onRun },
    { id: 'paste', label: t('snippet.paste'), icon: 'command', onSelect: onPaste },
    { type: 'separator' },
    { id: 'edit', label: t('snippet.edit'), icon: 'edit', onSelect: onEdit },
    { id: 'del', label: t('snippet.delete'), icon: 'trash', danger: true, onSelect: onDelete },
  ]);
  return (
    <>
      <button
        type="button"
        className="terminal-chip"
        style={{ ['--h' as string]: `var(--h-${look.hue})`, ['--s' as string]: `var(--h-${look.hue}-s)` }}
        title={`${s.command}\n${t('snippet.tip')}`}
        // single click pastes; a quick second click runs. The paste waits so a double click does not paste twice.
        onClick={(e) => {
          if (e.detail > 1) return;
          window.clearTimeout(timer.current);
          timer.current = window.setTimeout(onPaste, 240);
        }}
        onDoubleClick={() => {
          window.clearTimeout(timer.current);
          onRun();
        }}
        {...cm.bind}
      >
        <Icon name={look.icon} />
        {s.title}
      </button>
      {cm.menu}
    </>
  );
}

export default function Snippets(p: { snippets: Snippet[]; onPaste(s: Snippet): void; onRun(s: Snippet): void; onEdit(s: Snippet): void; onDelete(s: Snippet): void; onAdd(): void }) {
  const t = useT('terminal');
  return (
    <div className="terminal-chips" role="toolbar" aria-label={t('snippet.bar')}>
      {p.snippets.map((s) => (
        <Chip key={s.id} s={s} onPaste={() => p.onPaste(s)} onRun={() => p.onRun(s)} onEdit={() => p.onEdit(s)} onDelete={() => p.onDelete(s)} />
      ))}
      <button type="button" className="terminal-chip terminal-chip-add" onClick={p.onAdd}>
        <Icon name="plus" />
        {t('snippet.add')}
      </button>
    </div>
  );
}
