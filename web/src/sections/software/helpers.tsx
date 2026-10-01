import type { ReactNode } from 'react';
import { useT } from '../../i18n';
import type { Kind, Update } from './types';

export const kindLabel = (kind: Kind, source: string): string => (kind === 'flatpak' ? 'Flatpak' : kind === 'aur' ? 'AUR' : source);

/** Coloured source badge: repositories violet, AUR amber, Flatpak blue. */
export function SourceBadge({ kind, source }: { kind: Kind; source: string }) {
  return <span className={`sw-src sw-src--${kind}`}>{kindLabel(kind, source)}</span>;
}

/** Notes of an update: reboot, security fix, service restart. */
export function Notes({ u }: { u: Update }) {
  const t = useT('software');
  const out: ReactNode[] = [];
  for (const n of u.notes) {
    if (n === 'reboot') out.push(<span key={n} className="sw-note sw-note--warn">{t('note.reboot')}</span>);
    else if (n === 'security') out.push(<span key={n} className="sw-note sw-note--err">{t('note.security')}</span>);
    else if (n.startsWith('restartService:')) out.push(<span key={n} className="sw-note sw-note--warn">{t('note.restart', { unit: n.slice(15) })}</span>);
  }
  return <>{out}</>;
}

export const updateKey = (u: { kind: Kind; scope?: string; name: string }) => `${u.kind}:${u.scope ?? ''}:${u.name}`;

/** Letters-only package label for tile fallback. */
export const displayName = (x: { name: string; title?: string }) => x.title || x.name;

export function groupBy<T>(list: T[], key: (x: T) => string): Map<string, T[]> {
  const m = new Map<string, T[]>();
  for (const x of list) {
    const k = key(x);
    const a = m.get(k);
    if (a) a.push(x);
    else m.set(k, [x]);
  }
  return m;
}
