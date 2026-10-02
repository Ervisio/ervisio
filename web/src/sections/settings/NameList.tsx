import { useState } from 'react';
import { useT } from '../../i18n';
import { Icon, Input } from '../../ui';

/** Same shape the server accepts for user and group names (config keys auth.allow_users / auth.allow_groups). */
export const NAME_RE = /^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}\$?$/;

/** Editable list of user or group names shown as removable chips; every change is saved at once. */
export function NameList({ names, onChange, disabled, label, placeholder }: { names: string[]; onChange(next: string[]): void; disabled?: boolean; label: string; placeholder: string }) {
  const t = useT('settings');
  const [draft, setDraft] = useState('');
  const [err, setErr] = useState('');
  const add = () => {
    // Commas and spaces separate several names pasted at once.
    const parts = draft.split(/[\s,]+/).filter(Boolean);
    if (!parts.length) return;
    const bad = parts.find((p) => !NAME_RE.test(p));
    if (bad) { setErr(t('signin.badName', { name: bad })); return; }
    const next = [...names];
    for (const p of parts) if (!next.includes(p)) next.push(p);
    setDraft('');
    setErr('');
    if (next.length !== names.length) onChange(next);
  };
  return (
    <div className="st-names">
      {names.length > 0 && (
        <ul className="st-chips" aria-label={label}>
          {names.map((n) => (
            <li key={n} className="st-chip">
              <span>{n}</span>
              <button type="button" disabled={disabled} aria-label={t('signin.removeName', { name: n })} onClick={() => onChange(names.filter((x) => x !== n))}>
                <Icon name="x" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <Input
        compact
        mono
        aria-label={label}
        placeholder={placeholder}
        disabled={disabled}
        value={draft}
        error={err || undefined}
        onChange={(e) => { setDraft(e.target.value); setErr(''); }}
        onBlur={add}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add(); } }}
      />
    </div>
  );
}
