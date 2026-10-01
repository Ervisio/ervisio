import { useCallback, useEffect, useMemo, useState } from 'react';
import { call, usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import { Button, EmptyState, Icon, Input, Page, Segmented, Skeleton, Table, Tabs, useIsMobile, type Column } from '../../ui';
import { usePaletteActions } from '../index';
import { CreateDialog } from './CreateDialog';
import { GroupsView } from './GroupsView';
import { Avatar, RoleBadge, useLastLogin } from './parts';
import type { GroupInfo, UserInfo, UserList } from './types';
import { UserPanel } from './UserPanel';
import { hueOf, initial, roleOf } from './util';
import './users.css';

type Tab = 'people' | 'system' | 'groups';

export default function UsersPage() {
  const t = useT('users');
  const { session, isUnlocked } = useSession();
  const { prefs, set } = usePrefs();
  const view: 'cards' | 'table' = (prefs as Record<string, unknown>)['users.view'] === 'table' ? 'table' : 'cards';
  const lastLogin = useLastLogin();
  const mobile = useIsMobile();

  const [tab, setTab] = useState<Tab>('people');
  const [data, setData] = useState<UserList | null>(null);
  const [groups, setGroups] = useState<GroupInfo[]>([]);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('');
  const [selName, setSelName] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [groupOpen, setGroupOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      const [u, g] = await Promise.all([
        call<UserList>('users.list', {}, { admin: isUnlocked }),
        call<GroupInfo[]>('users.groups'),
      ]);
      setData(u);
      setGroups(g);
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [isUnlocked]);
  useEffect(() => { void load(); }, [load]);

  usePaletteActions('users', [
    { id: 'users.add', title: t('palette.add'), hint: t('title'), icon: 'plus', hue: 'usr', keywords: [t('palette.addKeywords')], run: () => setCreateOpen(true) },
    { id: 'users.addGroup', title: t('palette.addGroup'), hint: t('title'), icon: 'users', hue: 'usr', run: () => { setTab('groups'); setGroupOpen(true); } },
  ]);

  const self = session?.user ?? data?.self ?? '';
  const q = filter.trim().toLowerCase();
  const match = (u: UserInfo) => !q || u.name.toLowerCase().includes(q) || u.fullName.toLowerCase().includes(q);
  const people = useMemo(() => (data?.people ?? []).filter(match), [data, q]); // eslint-disable-line react-hooks/exhaustive-deps
  const system = useMemo(() => (data?.system ?? []).filter(match), [data, q]); // eslint-disable-line react-hooks/exhaustive-deps
  const selected = data?.people.find((u) => u.name === selName) ?? null;

  const peopleCols: Column<UserInfo>[] = [
    { key: 'name', header: t('table.user'), value: (u) => u.name, sortable: true, render: (u) => <div className="usr-namecell"><Avatar name={u.name} /><b>{u.name}</b></div> },
    { key: 'full', header: t('table.fullName'), value: (u) => u.fullName, sortable: true },
    { key: 'role', header: t('table.role'), value: (u) => roleOf(u), sortable: true, render: (u) => <RoleBadge user={u} /> },
    { key: 'uid', header: 'UID', value: (u) => u.uid, sortable: true, align: 'right' },
    { key: 'groups', header: t('table.groups'), render: (u) => <>{u.groups.slice(0, 5).map((g) => <span key={g} className={`usr-gch${['wheel', 'sudo', 'admin'].includes(g) ? ' w' : ''}`}>{g}</span>)}{u.groups.length > 5 && <span className="usr-muted">+{u.groups.length - 5}</span>}</> },
    { key: 'last', header: t('table.lastLogin'), value: (u) => u.lastLogin?.at ?? 0, sortable: true, render: (u) => lastLogin(u) },
    { key: 'shell', header: t('table.shell'), value: (u) => u.shell, mono: true },
  ];
  const sysCols: Column<UserInfo>[] = [
    { key: 'name', header: t('table.user'), value: (u) => u.name, sortable: true, mono: true },
    { key: 'uid', header: 'UID', value: (u) => u.uid, sortable: true, align: 'right' },
    { key: 'gid', header: 'GID', value: (u) => u.gid, sortable: true, align: 'right' },
    { key: 'desc', header: t('table.fullName'), value: (u) => u.fullName },
    { key: 'home', header: t('table.home'), value: (u) => u.home, mono: true },
    { key: 'shell', header: t('table.shell'), value: (u) => u.shell, mono: true },
  ];

  const onCreated = async (name: string) => {
    setCreateOpen(false);
    await load();
    setTab('people');
    setSelName(name);
  };

  return (
    <Page flush hue="usr">
      <div className="usr-root hue-usr">
        <div className="usr-main">
          <div className="usr-bar">
            <div className="usr-tabs"><Tabs<Tab>
              variant="pill"
              hue="usr"
              aria-label={t('tabs.label')}
              value={tab}
              onChange={(v) => { setTab(v); setFilter(''); }}
              items={[
                { id: 'people', label: t('tabs.people'), count: data?.people.length },
                { id: 'system', label: t('tabs.system'), count: data?.system.length },
                { id: 'groups', label: t('tabs.groups'), count: groups.length },
              ]}
            /></div>
            <div className="usr-sp" />
            {tab === 'people' && (
              <Segmented<'cards' | 'table'>
                aria-label={t('view.label')}
                value={view}
                onChange={(v) => void set('users.view', v)}
                options={[{ value: 'cards', label: t('view.cards'), icon: 'grid' }, { value: 'table', label: t('view.table'), icon: 'list' }]}
              />
            )}
            <div className="usr-filter">
              <Input compact icon="search" aria-label={t('filter')} placeholder={tab === 'groups' ? t('filterGroups') : t('filter')} value={filter} onChange={(e) => setFilter(e.target.value)} />
            </div>
          </div>

          {error && (
            <div className="usr-note err" role="alert"><Icon name="alert" />{error}<Button size="sm" onClick={() => void load()}>{t('retry')}</Button></div>
          )}
          {data && !data.shadowReadable && tab === 'people' && (
            <div className="usr-note"><Icon name="info" />{t('notes.shadow')}</div>
          )}

          {!data && !error && <div className="usr-grid">{[0, 1, 2, 3].map((i) => <Skeleton key={i} height={200} />)}</div>}

          {data && tab === 'people' && view === 'cards' && (
            <div className="usr-grid">
              {people.map((u) => {
                const r = roleOf(u);
                return (
                  <button type="button" key={u.name} className={`usr-tile ${hueOf(u.name)}${selName === u.name ? ' on' : ''}${r === 'disabled' ? ' off' : ''}`} onClick={() => setSelName(selName === u.name ? null : u.name)} aria-pressed={selName === u.name}>
                    <span className="usr-big" aria-hidden="true">{initial(u.name)}</span>
                    <b>{u.name}</b>
                    <small>{u.fullName}</small>
                    <RoleBadge user={u} />
                    <span className="usr-ll">{lastLogin(u)}</span>
                  </button>
                );
              })}
              {!q && (
                <button type="button" className="usr-tile usr-add" onClick={() => setCreateOpen(true)}>
                  <span className="usr-big"><Icon name="plus" /></span>
                  <b>{t('add.title')}</b>
                  <small>{t('add.text')}</small>
                </button>
              )}
              {q && people.length === 0 && <EmptyState icon="search" hue="usr" title={t('emptyFilter')} />}
            </div>
          )}

          {data && tab === 'people' && view === 'table' && (
            <div className="usr-tablewrap">
              <div style={{ marginBottom: 12 }}><Button icon="plus" onClick={() => setCreateOpen(true)}>{t('add.title')}</Button></div>
              <Table<UserInfo>
                columns={mobile ? peopleCols.filter((c) => ['name', 'role'].includes(c.key)) : peopleCols}
                rows={people}
                rowKey={(u) => u.name}
                activeKey={selName ?? undefined}
                onRowClick={(u) => setSelName(selName === u.name ? null : u.name)}
                empty={<EmptyState icon="search" hue="usr" title={t('emptyFilter')} />}
              />
            </div>
          )}

          {data && tab === 'system' && (
            <div className="usr-tablewrap">
              <p className="usr-muted" style={{ marginBottom: 12 }}>{t('system.text')}</p>
              <Table<UserInfo> columns={mobile ? sysCols.filter((c) => ['name', 'uid', 'shell'].includes(c.key)) : sysCols} rows={system} rowKey={(u) => u.name} empty={<EmptyState icon="search" hue="usr" title={t('emptyFilter')} />} />
            </div>
          )}

          {data && tab === 'groups' && (
            <GroupsView groups={groups} people={data.people} filter={filter} reload={load} newOpen={groupOpen} setNewOpen={setGroupOpen} />
          )}
        </div>

        {data && tab === 'people' && (
          <UserPanel user={selected} data={data} groups={groups} self={self} onClose={() => setSelName(null)} reload={load} />
        )}
      </div>
      {data && <CreateDialog open={createOpen} onClose={() => setCreateOpen(false)} data={data} groups={groups} onCreated={onCreated} />}
    </Page>
  );
}
