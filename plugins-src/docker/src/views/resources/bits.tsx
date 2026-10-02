import { containerName } from '../../api/format';
import type { Container } from '../../api/types';
import { t } from '../../i18n';
import { IconButton, toast } from '../../kit';
import { navigate } from '../../router';

/** Copies text; falls back to a toast with the text when the browser refuses. */
export async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    toast.ok(t('res.copied'));
  } catch {
    toast.info(t('res.copyFailed'), text);
  }
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  return <IconButton icon="copy" size="sm" variant="ghost" label={label ?? t('common.copy')} onClick={() => void copyText(text)} />;
}

/** Container chips that open the container page. Shows `unused` when the list is empty. */
export function UsedBy({ list, unusedLabel, max = 4 }: { list: Container[]; unusedLabel?: string; max?: number }) {
  if (!list.length) return <div className="dk-used"><span className="dk-unused">{unusedLabel ?? t('res.unused')}</span></div>;
  return (
    <div className="dk-used">
      {list.slice(0, max).map((c) => (
        <button key={c.Id} type="button" className={`dk-uchip${c.State === 'running' ? '' : ' dk-uchip--off'}`} title={c.Status} onClick={(e) => { e.stopPropagation(); navigate({ view: 'container', id: c.Id }); }}>
          {containerName(c)}
        </button>
      ))}
      {list.length > max && <span className="dk-more">+{list.length - max}</span>}
    </div>
  );
}

/** Text filter match, case insensitive, over several fields. */
export const matchesText = (q: string, ...fields: (string | undefined | null)[]): boolean => {
  const s = q.trim().toLowerCase();
  return !s || fields.some((f) => f?.toLowerCase().includes(s));
};

/** Anonymous volumes have a 64 character hex name. */
export const isAnonymousVolume = (name: string): boolean => /^[a-f0-9]{64}$/.test(name);

/** Which containers use which thing, keyed by `key(c)` values. */
export function groupContainers(list: Container[], keys: (c: Container) => string[]): Map<string, Container[]> {
  const m = new Map<string, Container[]>();
  for (const c of list) for (const k of new Set(keys(c))) {
    const a = m.get(k);
    if (a) a.push(c);
    else m.set(k, [c]);
  }
  return m;
}
