import { useState, type ReactNode } from 'react';
import { useT } from '../../i18n';
import { Button, EmptyState, Icon, Input, Segmented, Skeleton, type IconName } from '../../ui';
import { Avatar, RoleBadge, useLastLogin } from './parts';
import type { UserInfo, UserList } from './types';
import type { UserDetailTab } from './UserPanel';
import './native-users.css';

export type NativeUsersSection = 'people' | 'system' | 'groups' | 'security';
type Props = {
  data: UserList | null;
  people: UserInfo[];
  groupCount: number;
  section: NativeUsersSection;
  onSection(section: NativeUsersSection): void;
  filter: string;
  onFilter(value: string): void;
  view: 'cards' | 'table';
  onView(view: 'cards' | 'table'): void;
  selected: string | null;
  onSelect(name: string, tab?: UserDetailTab): void;
  onAddUser(): void;
  onAddGroup(): void;
  onRefresh(): Promise<void>;
  self: string;
  error: string;
  host: string;
  children: ReactNode;
  details: ReactNode;
};

const sections: { id: NativeUsersSection; icon: IconName; key: string }[] = [
  { id: 'people', icon: 'user', key: 'tabs.people' },
  { id: 'system', icon: 'server', key: 'tabs.system' },
  { id: 'groups', icon: 'users', key: 'tabs.groups' },
  { id: 'security', icon: 'shield', key: 'native.security' },
];

function AccountCards({
  people,
  selected,
  onSelect,
}: Pick<Props, 'people' | 'selected' | 'onSelect'>) {
  const t = useT('users');
  const lastLogin = useLastLogin();
  if (!people.length) return <EmptyState icon="search" title={t('emptyFilter')} />;
  return (
    <div className="usr-native-accounts">
      {people.map((user) => (
        <button
          type="button"
          key={user.name}
          className={`usr-native-account${selected === user.name ? ' is-selected' : ''}`}
          aria-pressed={selected === user.name}
          onClick={() => onSelect(user.name, 'info')}
        >
          <span className="usr-native-account-head">
            <Avatar name={user.name} large />
            <span>
              <b>{user.fullName || user.name}</b>
              <small>{user.name}</small>
            </span>
            <Icon name="right" size={15} />
          </span>
          <span className="usr-native-account-meta">
            <RoleBadge user={user} />
            <code>UID {user.uid}</code>
          </span>
          <span className="usr-native-account-home">
            <Icon name="files" size={14} />
            <code>{user.home}</code>
          </span>
          <span className="usr-native-account-login">{lastLogin(user)}</span>
          <span className="usr-native-account-groups">
            {user.groups.slice(0, 4).map((group) => (
              <span key={group}>{group}</span>
            ))}
            {user.groups.length > 4 && <span>+{user.groups.length - 4}</span>}
          </span>
        </button>
      ))}
    </div>
  );
}

function Security({ data, people, onSelect }: Pick<Props, 'data' | 'people' | 'onSelect'>) {
  const t = useT('users');
  if (!data) return null;
  const admins = data.people.filter((user) => user.isAdmin).length;
  const restricted = data.people.filter(
    (user) => user.locked || user.expired || user.noLoginShell,
  ).length;
  return (
    <div className="usr-native-security">
      <div className="usr-native-security-summary">
        <Icon name="shield" size={23} />
        <div>
          <b>{t('native.accessSummary')}</b>
          <span>{t('native.accessCounts', { admins, restricted, total: data.people.length })}</span>
        </div>
      </div>
      <p className="usr-native-help">{t('native.securityText')}</p>
      {!people.length ? (
        <EmptyState icon="search" title={t('emptyFilter')} />
      ) : (
        <div className="usr-native-access-list">
          {people.map((user) => (
            <div className="usr-native-access" key={user.name}>
              <button
                type="button"
                className="usr-native-access-person"
                onClick={() => onSelect(user.name, 'info')}
              >
                <Avatar name={user.name} />
                <span>
                  <b>{user.fullName || user.name}</b>
                  <small>{user.name}</small>
                </span>
              </button>
              <RoleBadge user={user} />
              <span className="usr-native-access-actions">
                <Button
                  size="sm"
                  variant="ghost"
                  icon="key"
                  onClick={() => onSelect(user.name, 'keys')}
                >
                  {t('tabs.keys')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  icon="terminal"
                  onClick={() => onSelect(user.name, 'sess')}
                >
                  {t('tabs.sessions')}
                </Button>
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** Native account workspace, using the same data and mutation panels as Classic. */
export function NativeUsers(props: Props) {
  const t = useT('users');
  const [refreshing, setRefreshing] = useState(false);
  const current = props.data?.people.find((user) => user.name === props.self);
  const title = sections.find((section) => section.id === props.section)!;
  const count = (id: NativeUsersSection) =>
    id === 'people'
      ? props.data?.people.length
      : id === 'system'
        ? props.data?.system.length
        : id === 'groups'
          ? props.groupCount
          : undefined;
  return (
    <div className="usr-native hue-usr">
      <div className="usr-native-layout">
        <aside className="usr-native-sidebar" aria-label={t('native.navigation')}>
          <div className="usr-native-identity">
            <Icon name="users" size={22} />
            <span>
              <b>{t('title')}</b>
              <small>{props.host}</small>
            </span>
          </div>
          {current && (
            <button
              type="button"
              className="usr-native-self"
              onClick={() => props.onSelect(current.name, 'info')}
            >
              <Avatar name={current.name} />
              <span>
                <small>{t('native.signedIn')}</small>
                <b>{current.fullName || current.name}</b>
              </span>
            </button>
          )}
          <nav>
            {sections.map((section) => (
              <button
                type="button"
                key={section.id}
                className={props.section === section.id ? 'is-active' : ''}
                aria-current={props.section === section.id ? 'page' : undefined}
                onClick={() => props.onSection(section.id)}
              >
                <Icon name={section.icon} size={17} />
                <span>{t(section.key)}</span>
                {count(section.id) !== undefined && <small>{count(section.id)}</small>}
              </button>
            ))}
          </nav>
          <div className="usr-native-privilege">
            <Icon name={props.data?.shadowReadable ? 'unlock' : 'lock'} size={16} />
            <span>{t(props.data?.shadowReadable ? 'native.privileged' : 'native.standard')}</span>
          </div>
        </aside>
        <div className="usr-native-right">
          <header className="usr-native-commandbar">
            <Button
              size="sm"
              variant="primary"
              icon="plus"
              disabled={!props.data}
              onClick={props.section === 'groups' ? props.onAddGroup : props.onAddUser}
            >
              {t(props.section === 'groups' ? 'groups.newTitle' : 'add.title')}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon="refresh"
              loading={refreshing}
              aria-label={t('native.refresh')}
              onClick={() => {
                setRefreshing(true);
                void props.onRefresh().finally(() => setRefreshing(false));
              }}
            />
            <span className="usr-native-command-spacer" />
            {props.section === 'people' && (
              <Segmented<'cards' | 'table'>
                aria-label={t('view.label')}
                value={props.view}
                onChange={props.onView}
                options={[
                  { value: 'cards', icon: 'grid', label: t('view.cards') },
                  { value: 'table', icon: 'list', label: t('view.table') },
                ]}
              />
            )}
            <Input
              compact
              icon="search"
              fieldClassName="usr-native-search"
              aria-label={props.section === 'groups' ? t('filterGroups') : t('filter')}
              placeholder={props.section === 'groups' ? t('filterGroups') : t('filter')}
              value={props.filter}
              onChange={(event) => props.onFilter(event.target.value)}
            />
          </header>
          <div className="usr-native-workspace">
            <main className="usr-native-content">
              <div className="usr-native-heading">
                <h1>{t(title.key)}</h1>
                <p>{t(`native.description.${props.section}`)}</p>
              </div>
              {props.error && (
                <div className="usr-note err" role="alert">
                  <Icon name="alert" />
                  {props.error}
                  <Button size="sm" onClick={() => void props.onRefresh()}>
                    {t('retry')}
                  </Button>
                </div>
              )}
              {props.data &&
                !props.data.shadowReadable &&
                (props.section === 'people' || props.section === 'security') && (
                  <div className="usr-note">
                    <Icon name="info" />
                    {t('notes.shadow')}
                  </div>
                )}
              {!props.data && !props.error && (
                <div className="usr-native-skeleton">
                  <Skeleton height={120} />
                  <Skeleton height={120} />
                  <Skeleton height={120} />
                </div>
              )}
              {props.data && props.section === 'people' && props.view === 'cards' && (
                <AccountCards
                  people={props.people}
                  selected={props.selected}
                  onSelect={props.onSelect}
                />
              )}
              {props.data && props.section === 'security' && (
                <Security data={props.data} people={props.people} onSelect={props.onSelect} />
              )}
              {props.children}
            </main>
            {props.details}
          </div>
          <footer className="usr-native-status">
            <span>{props.host}</span>
            <span>
              {props.data
                ? t('native.accountCounts', {
                    people: props.data.people.length,
                    system: props.data.system.length,
                    groups: props.groupCount,
                  })
                : t(props.error ? 'errors.failed' : 'native.loading')}
            </span>
          </footer>
        </div>
      </div>
    </div>
  );
}
