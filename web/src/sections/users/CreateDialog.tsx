import { useState, type FormEvent } from 'react';
import { call, ApiError } from '../../api';
import { useT } from '../../i18n';
import { Button, Checkbox, Chip, Dialog, Icon, Input, Select, Switch, toast } from '../../ui';
import type { GroupInfo, UserList } from './types';
import { ADMIN_GROUPS, COMMON_GROUPS, NAME_RE, strength } from './util';

export function StrengthHint({ password }: { password: string }) {
  const t = useT('users');
  const s = strength(password);
  return (
    <div className="usr-str" aria-live="polite">
      {[1, 2, 3, 4].map((i) => <i key={i} className={s >= i ? `on${s}` : ''} />)}
      <span>{s === 0 ? t('password.hint') : t(`password.strength${s}`)}</span>
    </div>
  );
}

export function CreateDialog({ open, onClose, data, groups, onCreated }: {
  open: boolean;
  onClose(): void;
  data: UserList;
  groups: GroupInfo[];
  onCreated(name: string): void;
}) {
  const t = useT('users');
  const [name, setName] = useState('');
  const [full, setFull] = useState('');
  const [pw, setPw] = useState('');
  const [pw2, setPw2] = useState('');
  const [admin, setAdmin] = useState(false);
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [shell, setShell] = useState('');
  const [home, setHome] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [touched, setTouched] = useState(false);

  const shells = data.shells.length ? data.shells : ['/bin/bash'];
  const curShell = shell || (shells.includes('/bin/bash') ? '/bin/bash' : shells[0]);
  const choices = groups.filter((g) => COMMON_GROUPS.has(g.name) && !ADMIN_GROUPS.includes(g.name));

  const nameErr = touched && !NAME_RE.test(name) ? t('create.nameInvalid') : data.people.concat(data.system).some((u) => u.name === name) ? t('create.nameTaken') : '';
  const pwErr = touched && !pw ? t('create.passwordRequired') : '';
  const pw2Err = touched && pw && pw !== pw2 ? t('create.passwordMismatch') : '';
  const fullErr = /[:,=\\]/.test(full) ? t('create.fullNameInvalid') : '';

  const reset = () => {
    setName(''); setFull(''); setPw(''); setPw2(''); setAdmin(false); setSel(new Set()); setShell(''); setHome(true);
    setErr(''); setTouched(false);
  };
  const close = () => { if (!busy) { reset(); onClose(); } };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!NAME_RE.test(name) || nameErr || !pw || pw !== pw2 || fullErr) return;
    setBusy(true);
    setErr('');
    try {
      await call('users.create', { name, fullName: full.trim(), shell: curShell, password: pw, admin, groups: [...sel], createHome: home });
      toast.ok(t('create.done', { name }));
      reset();
      onCreated(name);
    } catch (x) {
      setErr(x instanceof ApiError || x instanceof Error ? x.message : String(x));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      title={t('create.title')}
      description={t('create.description')}
      icon="user"
      onSubmit={submit}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={busy}>{t('cancel')}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t('create.submit')}</Button>
        </>
      }
    >
      <div className="usr-form">
        <Input data-autofocus label={t('create.name')} value={name} onChange={(e) => setName(e.target.value.toLowerCase())} error={nameErr} hint={t('create.nameHint')} mono autoComplete="off" spellCheck={false} />
        <Input label={t('create.fullName')} value={full} onChange={(e) => setFull(e.target.value)} error={fullErr} autoComplete="off" />
        <div>
          <Input label={t('create.password')} type="password" value={pw} onChange={(e) => setPw(e.target.value)} error={pwErr} autoComplete="new-password" />
          <StrengthHint password={pw} />
        </div>
        <Input label={t('create.passwordConfirm')} type="password" value={pw2} onChange={(e) => setPw2(e.target.value)} error={pw2Err} autoComplete="new-password" />
        <Switch checked={admin} onChange={setAdmin} label={t('create.admin', { group: data.adminGroup || 'wheel' })} />
        {choices.length > 0 && (
          <div>
            <span className="usr-label">{t('create.groups')}</span>
            <div className="usr-chips">
              {choices.map((g) => (
                <Chip key={g.name} pressed={sel.has(g.name)} onClick={() => setSel((s) => { const n = new Set(s); if (n.has(g.name)) n.delete(g.name); else n.add(g.name); return n; })}>{g.name}</Chip>
              ))}
            </div>
          </div>
        )}
        <Select label={t('create.shell')} value={curShell} onChange={setShell} options={shells.map((s) => ({ value: s, label: s }))} />
        <Checkbox checked={home} onChange={setHome} label={t('create.home')} />
        {err && <div className="ui-form-err" role="alert"><Icon name="alert" />{err}</div>}
      </div>
    </Dialog>
  );
}
