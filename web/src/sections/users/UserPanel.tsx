import { useCallback, useEffect, useState } from 'react';
import { call, ApiError } from '../../api';
import { useT, useI18n } from '../../i18n';
import { Button, Checkbox, ConfirmDialog, Dialog, EmptyState, Icon, IconButton, Input, Panel, Select, Skeleton, Switch, Tabs, Textarea, toast } from '../../ui';
import { relativeTime } from '../../lib/format';
import { Avatar, RoleBadge, useGroupDesc, useLastLogin } from './parts';
import { StrengthHint } from './CreateDialog';
import type { GroupInfo, SessionInfo, SshKey, UserInfo, UserList } from './types';
import { ADMIN_GROUPS, COMMON_GROUPS } from './util';

type Tab = 'info' | 'groups' | 'keys' | 'sess';

const msg = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e));

export function UserPanel({ user, data, groups, self, onClose, reload }: {
  user: UserInfo | null;
  data: UserList;
  groups: GroupInfo[];
  self: string;
  onClose(): void;
  reload(): Promise<void>;
}) {
  const t = useT('users');
  const [tab, setTab] = useState<Tab>('info');
  const [resetOpen, setResetOpen] = useState(false);
  const [delOpen, setDelOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const name = user?.name ?? '';
  useEffect(() => { setTab('info'); }, [name]);

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.ok(ok);
      await reload();
    } catch (e) {
      toast.err(t('errors.failed'), msg(e));
    } finally {
      setBusy(false);
    }
  };

  const isSelf = name === self;
  const isRoot = user?.uid === 0;
  const locked = user?.locked === true;

  return (
    <>
      <Panel
        inline
        open={!!user}
        onClose={onClose}
        hue="usr"
        title={user && <span className="usr-ptitle"><Avatar name={user.name} large /><span>{user.name}</span></span>}
        subtitle={user && t('panel.subtitle', { name: user.fullName || t('panel.noFullName'), uid: user.uid })}
        tabs={
          user && (
            <>
              <div className="usr-acts">
                <Button icon="key" onClick={() => setResetOpen(true)}>{t('actions.reset')}</Button>
                <Button
                  icon={locked ? 'unlock' : 'lock'}
                  disabled={busy || (!locked && isSelf)}
                  onClick={() => run(() => call(locked ? 'users.unlock' : 'users.lock', { name }), t(locked ? 'actions.unlocked' : 'actions.locked', { name }))}
                >
                  {t(locked ? 'actions.unlock' : 'actions.lock')}
                </Button>
                <Button variant="danger" icon="trash" disabled={isSelf || isRoot} onClick={() => setDelOpen(true)}>{t('actions.delete')}</Button>
              </div>
              <Tabs<Tab>
                value={tab}
                onChange={setTab}
                hue="usr"
                aria-label={t('panel.tabs')}
                items={[
                  { id: 'info', label: t('tabs.info') },
                  { id: 'groups', label: t('tabs.groups') },
                  { id: 'keys', label: t('tabs.keys') },
                  { id: 'sess', label: t('tabs.sessions') },
                ]}
              />
            </>
          )
        }
      >
        {user && tab === 'info' && <InfoTab user={user} data={data} self={self} run={run} />}
        {user && tab === 'groups' && <GroupsTab user={user} groups={groups} run={run} />}
        {user && tab === 'keys' && <KeysTab user={user} self={self} />}
        {user && tab === 'sess' && <SessionsTab user={user} />}
      </Panel>

      {user && <ResetDialog open={resetOpen} onClose={() => setResetOpen(false)} name={user.name} />}
      {user && (
        <DeleteDialog
          open={delOpen}
          onClose={() => setDelOpen(false)}
          name={user.name}
          onDone={async () => { onClose(); await reload(); }}
        />
      )}
    </>
  );
}

type RunFn = (fn: () => Promise<unknown>, ok: string) => Promise<void>;

function InfoTab({ user, data, self, run }: { user: UserInfo; data: UserList; self: string; run: RunFn }) {
  const t = useT('users');
  const lastLogin = useLastLogin();
  const [full, setFull] = useState(user.fullName);
  const [shell, setShell] = useState(user.shell);
  useEffect(() => { setFull(user.fullName); setShell(user.shell); }, [user.name, user.fullName, user.shell]);
  const dirty = full.trim() !== user.fullName || shell !== user.shell;
  const shells = data.shells.includes(user.shell) ? data.shells : [user.shell, ...data.shells];

  let pw: string;
  if (user.mustChange) pw = t('info.pwMustChange');
  else if (user.passwordState === 'unknown') pw = t('info.pwUnknown');
  else if (user.passwordState === 'disabled') pw = t('info.pwNone');
  else if (user.passwordState === 'empty') pw = t('info.pwEmpty');
  else if (user.passwordChanged != null) pw = user.passwordChanged === 0 ? t('info.pwToday') : t('info.pwChanged', { count: user.passwordChanged });
  else pw = t('info.pwUnknown');
  if (user.locked) pw = `${t('info.locked')}, ${pw}`;

  const save = () => {
    const body: Record<string, unknown> = { name: user.name };
    if (full.trim() !== user.fullName) body.fullName = full.trim();
    if (shell !== user.shell) body.shell = shell;
    return run(() => call('users.modify', body), t('info.saved', { name: user.name }));
  };

  return (
    <div className="usr-pp">
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <RoleBadge user={user} />
        {user.expired && <span className="usr-badge disabled">{t('info.expired')}</span>}
      </div>
      <Input label={t('info.fullName')} value={full} onChange={(e) => setFull(e.target.value)} />
      <Select label={t('info.shell')} value={shell} onChange={setShell} options={shells.map((s) => ({ value: s, label: s }))} />
      {dirty && (
        <div><Button variant="primary" onClick={save}>{t('info.save')}</Button></div>
      )}
      <dl className="usr-kv">
        <dt>{t('info.home')}</dt><dd className="usr-mono">{user.home}</dd>
        <dt>{t('info.primaryGroup')}</dt><dd className="usr-mono">{user.primaryGroup || user.gid}</dd>
        <dt>{t('info.password')}</dt><dd>{pw}</dd>
        <dt>{t('info.lastLogin')}</dt><dd>{lastLogin(user)}</dd>
      </dl>
      <label className="usr-row">
        <div>
          <b style={{ fontFamily: 'inherit' }}>{t('info.admin')}</b>
          <small>{t('info.adminHint', { group: data.adminGroup || 'wheel' })}</small>
        </div>
        <Switch
          checked={user.isAdmin}
          disabled={user.uid === 0}
          aria-label={t('info.admin')}
          onChange={(v) => run(() => call('users.setAdmin', { name: user.name, admin: v }), t(v ? 'info.adminOn' : 'info.adminOff', { name: user.name }))}
        />
      </label>
      {user.name === self && (
        <div className="usr-warn"><Icon name="alert" /><div>{t('info.selfWarning')}</div></div>
      )}
    </div>
  );
}

function GroupsTab({ user, groups, run }: { user: UserInfo; groups: GroupInfo[]; run: RunFn }) {
  const t = useT('users');
  const desc = useGroupDesc();
  const [all, setAll] = useState(false);
  const [newOpen, setNewOpen] = useState(false);
  const [newName, setNewName] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const member = (g: GroupInfo) => user.groups.includes(g.name) || g.name === user.primaryGroup;
  const shown = groups
    .filter((g) => all || member(g) || !g.system || ADMIN_GROUPS.includes(g.name) || COMMON_GROUPS.has(g.name))
    .sort((a, b) => {
      const rank = (g: GroupInfo) => (ADMIN_GROUPS.includes(g.name) ? 0 : member(g) ? 1 : 2);
      return rank(a) - rank(b) || a.name.localeCompare(b.name);
    });

  const toggle = (g: GroupInfo, on: boolean) =>
    run(
      () => call('users.modify', { name: user.name, groups: on ? { add: [g.name] } : { remove: [g.name] } }),
      t(on ? 'groups.added' : 'groups.removed', { name: user.name, group: g.name }),
    );

  const createGroup = async () => {
    setBusy(true);
    setErr('');
    try {
      await call('groups.create', { name: newName });
      await call('users.modify', { name: user.name, groups: { add: [newName] } });
      setNewOpen(false);
      setNewName('');
      await run(async () => undefined, t('groups.created', { group: newName }));
    } catch (e) {
      setErr(msg(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="usr-pp">
      {shown.map((g) => {
        const primary = g.name === user.primaryGroup;
        return (
          <label key={g.name} className={`usr-row${ADMIN_GROUPS.includes(g.name) ? ' hl' : ''}`}>
            <div>
              <b className="m">{g.name}</b>
              <small>{primary ? t('groups.primary') : desc(g) || ' '}</small>
            </div>
            <Switch checked={member(g)} disabled={primary} aria-label={g.name} onChange={(v) => toggle(g, v)} />
          </label>
        );
      })}
      <Checkbox checked={all} onChange={setAll} label={t('groups.showAll')} />
      <button type="button" className="usr-addline" onClick={() => setNewOpen(true)}><Icon name="plus" />{t('groups.new')}</button>
      <Dialog
        open={newOpen}
        onClose={() => !busy && setNewOpen(false)}
        title={t('groups.newTitle')}
        description={t('groups.newDescription', { name: user.name })}
        icon="users"
        onSubmit={(e) => { e.preventDefault(); void createGroup(); }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setNewOpen(false)} disabled={busy}>{t('cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!newName}>{t('groups.create')}</Button>
          </>
        }
      >
        <Input data-autofocus label={t('groups.name')} value={newName} onChange={(e) => setNewName(e.target.value.toLowerCase())} hint={t('create.nameHint')} mono autoComplete="off" />
        {err && <div className="ui-form-err" role="alert"><Icon name="alert" />{err}</div>}
      </Dialog>
    </div>
  );
}

function KeysTab({ user, self }: { user: UserInfo; self: string }) {
  const t = useT('users');
  const admin = user.name !== self;
  const [keys, setKeys] = useState<SshKey[] | null>(null);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState('');
  const [addErr, setAddErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [del, setDel] = useState<SshKey | null>(null);

  const load = useCallback(async () => {
    try {
      setKeys(await call<SshKey[]>('users.sshKeys', { name: user.name }, { admin }));
      setError('');
    } catch (e) {
      setError(msg(e));
      setKeys([]);
    }
  }, [user.name, admin]);
  useEffect(() => { setKeys(null); setAdding(false); setText(''); void load(); }, [load]);

  const add = async () => {
    setBusy(true);
    setAddErr('');
    try {
      await call('users.addSshKey', { name: user.name, key: text.trim() }, { admin });
      toast.ok(t('keys.added'));
      setText('');
      setAdding(false);
      await load();
    } catch (e) {
      setAddErr(msg(e));
    } finally {
      setBusy(false);
    }
  };

  if (keys === null) return <div className="usr-pp"><Skeleton height={64} /><Skeleton height={64} /></div>;
  return (
    <div className="usr-pp">
      {error && <div className="ui-form-err" role="alert"><Icon name="alert" />{error}</div>}
      {keys.length === 0 && !error && !adding && <EmptyState icon="key" hue="usr" title={t('keys.emptyTitle')} text={t('keys.emptyText')} />}
      {keys.map((k) => (
        <div className="usr-key" key={k.fingerprint + k.line}>
          <div className="usr-key-h">
            <b>{k.comment || k.type}</b>
            <IconButton icon="trash" label={t('keys.remove')} onClick={() => setDel(k)} />
          </div>
          <code>{k.type} {k.key.split(' ')[1]?.slice(0, 24)}…</code>
          <small className="usr-muted">{k.fingerprint}</small>
          {k.options && <small className="usr-muted">{t('keys.options')}: <span className="usr-mono">{k.options}</span></small>}
        </div>
      ))}
      {adding ? (
        <div className="usr-pp">
          <Textarea
            data-autofocus
            label={t('keys.paste')}
            value={text}
            rows={4}
            onChange={(e) => { setText(e.target.value); setAddErr(''); }}
            placeholder="ssh-ed25519 AAAA… name@host"
            error={addErr}
            mono
            spellCheck={false}
          />
          <div style={{ display: 'flex', gap: 8 }}>
            <Button variant="primary" loading={busy} disabled={!text.trim()} onClick={add}>{t('keys.add')}</Button>
            <Button variant="ghost" onClick={() => { setAdding(false); setText(''); setAddErr(''); }}>{t('cancel')}</Button>
          </div>
        </div>
      ) : (
        <button type="button" className="usr-addline" onClick={() => setAdding(true)}><Icon name="plus" />{t('keys.addKey')}</button>
      )}
      <ConfirmDialog
        open={!!del}
        onClose={() => setDel(null)}
        title={t('keys.removeTitle')}
        description={del && t('keys.removeText', { name: del.comment || del.type, user: user.name })}
        confirmLabel={t('keys.remove')}
        onConfirm={async () => {
          if (!del) return;
          await call('users.removeSshKey', { name: user.name, fingerprint: del.fingerprint }, { admin });
          toast.ok(t('keys.removed'));
          await load();
        }}
      />
    </div>
  );
}

function SessionsTab({ user }: { user: UserInfo }) {
  const t = useT('users');
  const { lang } = useI18n();
  const [list, setList] = useState<SessionInfo[] | null>(null);
  const [error, setError] = useState('');
  const load = useCallback(async () => {
    try {
      setList(await call<SessionInfo[]>('users.sessions', { name: user.name }));
      setError('');
    } catch (e) {
      setError(msg(e));
      setList([]);
    }
  }, [user.name]);
  useEffect(() => { setList(null); void load(); }, [load]);

  const end = async (s: SessionInfo) => {
    try {
      await call('users.terminateSession', { id: s.id });
      toast.ok(t('sessions.ended'));
      await load();
    } catch (e) {
      toast.err(t('errors.failed'), msg(e));
    }
  };

  if (list === null) return <div className="usr-pp"><Skeleton height={56} /><Skeleton height={56} /></div>;
  return (
    <div className="usr-pp">
      {error && <div className="ui-form-err" role="alert"><Icon name="alert" />{error}</div>}
      {list.length === 0 && !error && <EmptyState icon="terminal" hue="usr" title={t('sessions.emptyTitle')} text={t('sessions.emptyText', { name: user.name })} />}
      {list.map((s) => {
        const kind = s.remote || s.service === 'sshd' ? t('sessions.ssh') : s.type === 'tty' ? t('sessions.console') : t('sessions.desktop');
        const where = s.tty || s.type;
        return (
          <div className="usr-row" key={s.id}>
            <div>
              <b style={{ fontFamily: 'inherit' }}>{where ? `${kind}, ${where}` : kind}</b>
              <small>{[s.remoteHost || t('sessions.local'), s.since ? relativeTime(s.since * 1000, lang) : ''].filter(Boolean).join(', ')}</small>
            </div>
            <Button size="sm" onClick={() => end(s)}>{t('sessions.end')}</Button>
          </div>
        );
      })}
    </div>
  );
}

function ResetDialog({ open, onClose, name }: { open: boolean; onClose(): void; name: string }) {
  const t = useT('users');
  const [pw, setPw] = useState('');
  const [pw2, setPw2] = useState('');
  const [must, setMust] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  useEffect(() => { if (open) { setPw(''); setPw2(''); setMust(false); setErr(''); } }, [open]);
  const mismatch = pw2 !== '' && pw !== pw2;
  const submit = async () => {
    setBusy(true);
    setErr('');
    try {
      await call('users.setPassword', { name, password: pw, mustChange: must });
      toast.ok(t('reset.done', { name }));
      onClose();
    } catch (e) {
      setErr(msg(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={t('reset.title', { name })}
      icon="key"
      onSubmit={(e) => { e.preventDefault(); if (pw && !mismatch && pw === pw2) void submit(); }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>{t('cancel')}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!pw || pw !== pw2}>{t('reset.submit')}</Button>
        </>
      }
    >
      <div className="usr-form">
        <div>
          <Input data-autofocus label={t('create.password')} type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="new-password" />
          <StrengthHint password={pw} />
        </div>
        <Input label={t('create.passwordConfirm')} type="password" value={pw2} onChange={(e) => setPw2(e.target.value)} error={mismatch ? t('create.passwordMismatch') : undefined} autoComplete="new-password" />
        <Checkbox checked={must} onChange={setMust} label={t('reset.mustChange')} />
        {err && <div className="ui-form-err" role="alert"><Icon name="alert" />{err}</div>}
      </div>
    </Dialog>
  );
}

function DeleteDialog({ open, onClose, name, onDone }: { open: boolean; onClose(): void; name: string; onDone(): Promise<void> }) {
  const t = useT('users');
  const [keep, setKeep] = useState(true);
  useEffect(() => { if (open) setKeep(true); }, [open]);
  return (
    <ConfirmDialog
      open={open}
      onClose={onClose}
      title={t('delete.title', { name })}
      description={t('delete.text')}
      confirmLabel={t('delete.confirm')}
      confirmText={name}
      onConfirm={async () => {
        await call('users.delete', { name, removeHome: !keep });
        toast.ok(t('delete.done', { name }));
        await onDone();
      }}
    >
      <Checkbox checked={keep} onChange={setKeep} label={t('delete.keepHome')} />
    </ConfirmDialog>
  );
}

