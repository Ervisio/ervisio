import { useEffect, useRef, type ReactNode } from 'react';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { Icon, Input, Select } from '../../ui';
import type { GroupDef, RowDef } from './index';
import './settings-native.css';

interface Props {
  groups: GroupDef[];
  visible: { g: GroupDef; rows: RowDef[] }[];
  selected: string;
  query: string;
  locked: boolean;
  onQuery(value: string): void;
  onSelect(id: string): void;
  renderRow(row: RowDef): ReactNode;
}

/** Native category navigation around the same real controls, callbacks and privilege gates. */
export function NativeSettings(p: Props) {
  const t = useT('settings');
  const { session, host } = useSession();
  const content = useRef<HTMLDivElement>(null);
  const searching = !!p.query.trim();
  const selected = p.groups.find((group) => group.id === p.selected) ?? p.groups[0];
  const shown = searching ? p.visible : p.visible.filter(({ g }) => g.id === selected.id);
  const user = session?.name || session?.user || '';
  const administrative = !!session && (session.isRoot || session.isAdmin || session.canSudo);
  const resultCount = p.visible.reduce((count, item) => count + item.rows.length, 0);
  useEffect(() => {
    content.current?.scrollTo({ top: 0 });
  }, [p.selected, searching]);
  return (
    <div className="os-settings hue-set">
      <div className="os-settings-shell">
        <aside className="os-settings-nav">
          <div className="os-settings-account">
            <span>{user.slice(0, 1).toUpperCase()}</span>
            <div>
              <b>{user}</b>
              <small>{t(administrative ? 'native.adminAccount' : 'native.standardAccount')}</small>
            </div>
          </div>
          <Input
            icon="search"
            fieldClassName="os-settings-search"
            aria-label={t('search.placeholder')}
            placeholder={t('native.find')}
            value={p.query}
            onChange={(event) => p.onQuery(event.target.value)}
          />
          <nav aria-label={t('title')}>
            {(['you', 'server', 'about'] as const).map((part) => {
              const groups = p.groups.filter((group) => group.part === part);
              return groups.length ? (
                <div className="os-settings-nav-group" key={part}>
                  <span className="os-settings-nav-label">
                    {t(
                      part === 'you'
                        ? 'parts.you'
                        : part === 'server'
                          ? 'parts.server'
                          : 'native.product',
                    )}
                  </span>
                  {groups.map((group) => (
                    <button
                      type="button"
                      key={group.id}
                      data-settings-category={group.id}
                      aria-current={!searching && group.id === selected.id ? 'page' : undefined}
                      onClick={() => p.onSelect(group.id)}
                    >
                      <span className={`os-settings-nav-icon hue-${group.hue}`}>
                        <Icon name={group.icon} />
                      </span>
                      <span>{group.title}</span>
                      {group.admin && <Icon name="lock" className="os-settings-nav-lock" />}
                    </button>
                  ))}
                </div>
              ) : null;
            })}
          </nav>
          <div className="os-settings-device">
            <Icon name="server" size={16} />
            <div>
              <b>{host?.hostname}</b>
              <small>{host?.distro.name}</small>
            </div>
          </div>
        </aside>
        <div className="os-settings-content" ref={content}>
          <div className="os-settings-mobile-nav">
            <Input
              icon="search"
              aria-label={t('search.placeholder')}
              placeholder={t('native.find')}
              value={p.query}
              onChange={(event) => p.onQuery(event.target.value)}
            />
            <Select
              aria-label={t('native.category')}
              value={selected.id}
              options={p.groups.map((group) => ({ value: group.id, label: group.title }))}
              onChange={p.onSelect}
            />
          </div>
          <header className="os-settings-heading">
            <div className="os-settings-breadcrumb">
              <span>ErvisiOS</span>
              <Icon name="right" size={11} />
              <span>{t('title')}</span>
            </div>
            <div className="os-settings-title">
              <span className={`os-settings-category-icon hue-${selected.hue}`}>
                <Icon name={searching ? 'search' : selected.icon} size={25} />
              </span>
              <div>
                <h1>{searching ? t('native.results') : selected.title}</h1>
                <p>
                  {searching
                    ? t('native.resultCount', { count: resultCount })
                    : t(`native.descriptions.${selected.id}`)}
                </p>
              </div>
            </div>
          </header>
          {shown.length ? (
            shown.map(({ g, rows }) => (
              <section
                key={g.id}
                data-gid={g.id}
                className="os-settings-category"
                aria-label={g.title}
              >
                {searching && (
                  <button
                    type="button"
                    className="os-settings-result-category"
                    onClick={() => p.onSelect(g.id)}
                  >
                    <Icon name={g.icon} size={18} />
                    <h2>{g.title}</h2>
                    <Icon name="right" size={15} />
                  </button>
                )}
                {g.admin && (
                  <div className="os-settings-scope">
                    <Icon name={p.locked ? 'lock' : 'shield'} size={14} />
                    {t(p.locked ? 'native.adminLocked' : 'native.adminSettings')}
                  </div>
                )}
                <div className={`os-settings-rows${g.admin && p.locked ? ' st-grp--locked' : ''}`}>
                  {g.top}
                  {rows.map(p.renderRow)}
                </div>
              </section>
            ))
          ) : (
            <div className="os-settings-empty">
              <Icon name="search" size={32} />
              <p>{t('search.empty')}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
