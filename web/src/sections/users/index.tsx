import { useCallback, useEffect, useMemo, useState } from 'react';
import { call, usePrefs, useSession } from '../../api';
import { useT, type TFn } from '../../i18n';
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
type View = 'cards' | 'table';

const ADMIN_GROUPS = ['wheel', 'sudo', 'admin'];
const VISIBLE_GROUPS = 5;
const MOBILE_PEOPLE_COLS = ['name', 'role'];
const MOBILE_SYSTEM_COLS = ['name', 'uid', 'shell'];

function useUsersData(isUnlocked: boolean) {
  const [data, setData] = useState<UserList | null>(null);
  const [groups, setGroups] = useState<GroupInfo[]>([]);
  const [error, setError] = useState('');

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

  return { data, groups, error, load };
}

function filterUsers(users: UserInfo[] | undefined, q: string): UserInfo[] {
  const match = (u: UserInfo) => !q || u.name.toLowerCase().includes(q) || u.fullName.toLowerCase().includes(q);
  return (users ?? []).filter(match);
}

function GroupChips({ user }: { user: UserInfo }) {
  return (
    <>
      {user.groups.slice(0, VISIBLE_GROUPS).map((g) => <span key={g} className={`usr-gch${ADMIN_GROUPS.includes(g) ? ' w' : ''}`}>{g}</span>)}
      {user.groups.length > VISIBLE_GROUPS && <span className="usr-muted">+{user.groups.length - VISIBLE_GROUPS}</span>}
    </>
  );
}

function peopleColumns(t: TFn, lastLogin: (u: UserInfo) => string): Column<UserInfo>[] {
  return [
    { key: 'name', header: t('table.user'), value: (u) => u.name, sortable: true, render: (u) => <div className="usr-namecell"><Avatar name={u.name} /><b>{u.name}</b></div> },
    { key: 'full', header: t('table.fullName'), value: (u) => u.fullName, sortable: true },
    { key: 'role', header: t('table.role'), value: (u) => roleOf(u), sortable: true, render: (u) => <RoleBadge user={u} /> },
    { key: 'uid', header: 'UID', value: (u) => u.uid, sortable: true, align: 'right' },
    { key: 'groups', header: t('table.groups'), render: (u) => <GroupChips user={u} /> },
    { key: 'last', header: t('table.lastLogin'), value: (u) => u.lastLogin?.at ?? 0, sortable: true, render: (u) => lastLogin(u) },
    { key: 'shell', header: t('table.shell'), value: (u) => u.shell, mono: true },
  ];
}

function systemColumns(t: TFn): Column<UserInfo>[] {
  return [
    { key: 'name', header: t('table.user'), value: (u) => u.name, sortable: true, mono: true },
    { key: 'uid', header: 'UID', value: (u) => u.uid, sortable: true, align: 'right' },
    { key: 'gid', header: 'GID', value: (u) => u.gid, sortable: true, align: 'right' },
    { key: 'desc', header: t('table.fullName'), value: (u) => u.fullName },
    { key: 'home', header: t('table.home'), value: (u) => u.home, mono: true },
    { key: 'shell', header: t('table.shell'), value: (u) => u.shell, mono: true },
  ];
}

type ToolbarProps = {
  tab: Tab;
  onTab: (tab: Tab) => void;
  data: UserList | null;
  groupCount: number;
  view: View;
  onView: (view: View) => void;
  filter: string;
  onFilter: (filter: string) => void;
};

function UsersToolbar({ tab, onTab, data, groupCount, view, onView, filter, onFilter }: ToolbarProps) {
  const t = useT('users');
  return (
    <div className="usr-bar">
      <div className="usr-tabs"><Tabs<Tab>
        variant="pill"
        hue="usr"
        aria-label={t('tabs.label')}
        value={tab}
        onChange={onTab}
        items={[
          { id: 'people', label: t('tabs.people'), count: data?.people.length },
          { id: 'system', label: t('tabs.system'), count: data?.system.length },
          { id: 'groups', label: t('tabs.groups'), count: groupCount },
        ]}
      /></div>
      <div className="usr-sp" />
      {tab === 'people' && (
        <Segmented<View>
          aria-label={t('view.label')}
          value={view}
          onChange={onView}
          options={[{ value: 'cards', label: t('view.cards'), icon: 'grid' }, { value: 'table', label: t('view.table'), icon: 'list' }]}
        />
      )}
      <div className="usr-filter">
        <Input compact icon="search" aria-label={t('filter')} placeholder={tab === 'groups' ? t('filterGroups') : t('filter')} value={filter} onChange={(e) => onFilter(e.target.value)} />
      </div>
    </div>
  );
}

type PeopleProps = {
  people: UserInfo[];
  selName: string | null;
  onSelect: (name: string) => void;
  onAdd: () => void;
};

function PeopleCards({ people, selName, onSelect, onAdd, isFiltered }: PeopleProps & { isFiltered: boolean }) {
  const t = useT('users');
  const lastLogin = useLastLogin();
  return (
    <div className="usr-grid">
      {people.map((u) => {
        const r = roleOf(u);
        return (
          <button type="button" key={u.name} className={`usr-tile ${hueOf(u.name)}${selName === u.name ? ' on' : ''}${r === 'disabled' ? ' off' : ''}`} onClick={() => onSelect(u.name)} aria-pressed={selName === u.name}>
            <span className="usr-big" aria-hidden="true">{initial(u.name)}</span>
            <b>{u.name}</b>
            <small>{u.fullName}</small>
            <RoleBadge user={u} />
            <span className="usr-ll">{lastLogin(u)}</span>
          </button>
        );
      })}
      {!isFiltered && (
        <button type="button" className="usr-tile usr-add" onClick={onAdd}>
          <span className="usr-big"><Icon name="plus" /></span>
          <b>{t('add.title')}</b>
          <small>{t('add.text')}</small>
        </button>
      )}
      {isFiltered && people.length === 0 && <EmptyState icon="search" hue="usr" title={t('emptyFilter')} />}
    </div>
  );
}

function PeopleTable({ people, selName, onSelect, onAdd }: PeopleProps) {
  const t = useT('users');
  const lastLogin = useLastLogin();
  const mobile = useIsMobile();
  const cols = peopleColumns(t, lastLogin);
  return (
    <div className="usr-tablewrap">
      <div style={{ marginBottom: 12 }}><Button icon="plus" onClick={onAdd}>{t('add.title')}</Button></div>
      <Table<UserInfo>
        columns={mobile ? cols.filter((c) => MOBILE_PEOPLE_COLS.includes(c.key)) : cols}
        rows={people}
        rowKey={(u) => u.name}
        activeKey={selName ?? undefined}
        onRowClick={(u) => onSelect(u.name)}
        empty={<EmptyState icon="search" hue="usr" title={t('emptyFilter')} />}
      />
    </div>
  );
}

function SystemTable({ users }: { users: UserInfo[] }) {
  const t = useT('users');
  const mobile = useIsMobile();
  const cols = systemColumns(t);
  return (
    <div className="usr-tablewrap">
      <p className="usr-muted" style={{ marginBottom: 12 }}>{t('system.text')}</p>
      <Table<UserInfo> columns={mobile ? cols.filter((c) => MOBILE_SYSTEM_COLS.includes(c.key)) : cols} rows={users} rowKey={(u) => u.name} empty={<EmptyState icon="search" hue="usr" title={t('emptyFilter')} />} />
    </div>
  );
}

export default function UsersPage() {
  const t = useT('users');
  const { session, isUnlocked } = useSession();
  const { prefs, set } = usePrefs();
  const view: View = (prefs as Record<string, unknown>)['users.view'] === 'table' ? 'table' : 'cards';
  const { data, groups, error, load } = useUsersData(isUnlocked);

  const [tab, setTab] = useState<Tab>('people');
  const [filter, setFilter] = useState('');
  const [selName, setSelName] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [groupOpen, setGroupOpen] = useState(false);

  usePaletteActions('users', [
    { id: 'users.add', title: t('palette.add'), hint: t('title'), icon: 'plus', hue: 'usr', keywords: [t('palette.addKeywords')], run: () => setCreateOpen(true) },
    { id: 'users.addGroup', title: t('palette.addGroup'), hint: t('title'), icon: 'users', hue: 'usr', run: () => { setTab('groups'); setGroupOpen(true); } },
  ]);

  const self = session?.user ?? data?.self ?? '';
  const q = filter.trim().toLowerCase();
  const people = useMemo(() => filterUsers(data?.people, q), [data?.people, q]);
  const system = useMemo(() => filterUsers(data?.system, q), [data?.system, q]);
  const selected = data?.people.find((u) => u.name === selName) ?? null;

  const toggleSelected = (name: string) => setSelName(selName === name ? null : name);
  const openCreate = () => setCreateOpen(true);
  const changeTab = (v: Tab) => { setTab(v); setFilter(''); };

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
          <UsersToolbar
            tab={tab}
            onTab={changeTab}
            data={data}
            groupCount={groups.length}
            view={view}
            onView={(v) => void set('users.view', v)}
            filter={filter}
            onFilter={setFilter}
          />

          {error && (
            <div className="usr-note err" role="alert"><Icon name="alert" />{error}<Button size="sm" onClick={() => void load()}>{t('retry')}</Button></div>
          )}
          {data && !data.shadowReadable && tab === 'people' && (
            <div className="usr-note"><Icon name="info" />{t('notes.shadow')}</div>
          )}

          {!data && !error && <div className="usr-grid">{[0, 1, 2, 3].map((i) => <Skeleton key={i} height={200} />)}</div>}

          {data && tab === 'people' && view === 'cards' && (
            <PeopleCards people={people} selName={selName} onSelect={toggleSelected} onAdd={openCreate} isFiltered={!!q} />
          )}
          {data && tab === 'people' && view === 'table' && (
            <PeopleTable people={people} selName={selName} onSelect={toggleSelected} onAdd={openCreate} />
          )}
          {data && tab === 'system' && <SystemTable users={system} />}
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
