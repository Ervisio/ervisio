import { useState } from 'react';
import { call } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, ConfirmDialog, Dialog, EmptyState, Icon, IconButton, Input, Segmented, Select, toast } from '../../ui';
import { useGroupDesc } from './parts';
import type { GroupInfo, UserInfo } from './types';
import { NAME_RE } from './util';

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));

export function GroupsView({ groups, people, filter, reload, newOpen, setNewOpen }: {
  groups: GroupInfo[];
  people: UserInfo[];
  filter: string;
  reload(): Promise<void>;
  newOpen: boolean;
  setNewOpen(v: boolean): void;
}) {
  const t = useT('users');
  const desc = useGroupDesc();
  const [show, setShow] = useState<'inuse' | 'all'>('inuse');
  const [del, setDel] = useState<GroupInfo | null>(null);
  const [name, setName] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const q = filter.trim().toLowerCase();
  const peopleNames = new Set(people.map((p) => p.name));
  const list = groups.filter((g) => {
    if (q) return g.name.includes(q) || g.members.some((m) => m.includes(q));
    return show === 'all' || g.members.length > 0 || !g.system || g.primaryMembers.some((m) => peopleNames.has(m));
  });

  const change = async (method: 'groups.addMember' | 'groups.removeMember', group: string, user: string) => {
    try {
      await call(method, { group, user });
      toast.ok(t(method === 'groups.addMember' ? 'groups.added' : 'groups.removed', { name: user, group }));
      await reload();
    } catch (e) {
      toast.err(t('errors.failed'), msg(e));
    }
  };

  const create = async () => {
    setBusy(true);
    setErr('');
    try {
      await call('groups.create', { name });
      toast.ok(t('groups.created', { group: name }));
      setNewOpen(false);
      setName('');
      setShow('all');
      await reload();
    } catch (e) {
      setErr(msg(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="usr-bar" style={{ paddingTop: 4 }}>
        <Segmented<'inuse' | 'all'> aria-label={t('groups.show')} value={show} onChange={setShow} options={[{ value: 'inuse', label: t('groups.inUse') }, { value: 'all', label: t('groups.all') }]} />
        <div className="usr-sp" />
        <Button icon="plus" onClick={() => setNewOpen(true)}>{t('groups.newTitle')}</Button>
      </div>
      {list.length === 0 ? (
        <EmptyState icon="users" hue="usr" title={t('groups.emptyTitle')} text={t('groups.emptyText')} />
      ) : (
        <div className="usr-glist">
          {list.map((g) => {
            const candidates = people.filter((p) => !g.members.includes(p.name) && p.gid !== g.gid);
            return (
              <div className="usr-group" key={g.name}>
                <div className="usr-group-h">
                  <b>{g.name}</b>
                  <small>GID {g.gid}</small>
                  {g.system && <Badge>{t('groups.system')}</Badge>}
                  <span className="usr-sp" />
                  {!g.system && <IconButton icon="trash" label={t('groups.delete')} onClick={() => setDel(g)} />}
                </div>
                <p>{desc(g)}</p>
                <div className="usr-chips">
                  {g.primaryMembers.map((m) => <span key={`p${m}`} className="usr-member p" title={t('groups.primaryOf', { name: m })}>{m}</span>)}
                  {g.members.map((m) => (
                    <span key={m} className="usr-member">
                      {m}
                      <button type="button" aria-label={t('groups.removeMember', { name: m, group: g.name })} onClick={() => change('groups.removeMember', g.name, m)}><Icon name="x" /></button>
                    </span>
                  ))}
                  {g.members.length === 0 && g.primaryMembers.length === 0 && <span className="usr-muted">{t('groups.noMembers')}</span>}
                </div>
                {candidates.length > 0 && g.gid !== 0 && (
                  <Select
                    compact
                    aria-label={t('groups.addMember', { group: g.name })}
                    value=""
                    onChange={(v) => v && change('groups.addMember', g.name, v)}
                    options={[{ value: '', label: t('groups.addMemberPlaceholder') }, ...candidates.map((p) => ({ value: p.name, label: p.name }))]}
                  />
                )}
              </div>
            );
          })}
        </div>
      )}
      <Dialog
        open={newOpen}
        onClose={() => !busy && setNewOpen(false)}
        title={t('groups.newTitle')}
        icon="users"
        onSubmit={(e) => { e.preventDefault(); if (NAME_RE.test(name)) void create(); }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setNewOpen(false)} disabled={busy}>{t('cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!NAME_RE.test(name)}>{t('groups.create')}</Button>
          </>
        }
      >
        <Input data-autofocus label={t('groups.name')} value={name} onChange={(e) => setName(e.target.value.toLowerCase())} hint={t('create.nameHint')} mono autoComplete="off" />
        {err && <div className="ui-form-err" role="alert"><Icon name="alert" />{err}</div>}
      </Dialog>
      <ConfirmDialog
        open={!!del}
        onClose={() => setDel(null)}
        title={t('groups.deleteTitle', { group: del?.name ?? '' })}
        description={t('groups.deleteText')}
        confirmLabel={t('groups.deleteConfirm')}
        confirmText={del?.name}
        onConfirm={async () => {
          if (!del) return;
          await call('groups.delete', { name: del.name });
          toast.ok(t('groups.deleted', { group: del.name }));
          await reload();
        }}
      />
    </>
  );
}
