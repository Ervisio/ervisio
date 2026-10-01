import { useT, useI18n } from '../../i18n';
import { Icon } from '../../ui';
import { relativeTime } from '../../lib/format';
import type { GroupInfo, UserInfo } from './types';
import { hueOf, initial, roleOf, KNOWN_GROUPS } from './util';

export function Avatar({ name, large }: { name: string; large?: boolean }) {
  return <span className={`usr-av ${large ? 'lg' : ''} ${hueOf(name)}`} aria-hidden="true">{initial(name)}</span>;
}

export function RoleBadge({ user }: { user: UserInfo }) {
  const t = useT('users');
  const r = roleOf(user);
  return (
    <span className={`usr-badge ${r}`}>
      {r === 'admin' && <Icon name="shield" />}
      {r === 'disabled' && <Icon name="lock" />}
      {t(`role.${r}`)}
    </span>
  );
}

/** "Yesterday via 192.168.1.20", "Never", "Unknown". */
export function useLastLogin() {
  const t = useT('users');
  const { lang } = useI18n();
  return (u: UserInfo): string => {
    if (u.lastLogin) {
      const when = relativeTime(u.lastLogin.at * 1000, lang);
      const via = u.lastLogin.from && u.lastLogin.from !== ':0' ? u.lastLogin.from : u.lastLogin.tty;
      return via ? t('lastLogin.via', { when, via }) : when;
    }
    return u.neverLoggedIn ? t('lastLogin.never') : t('lastLogin.unknown');
  };
}

export function useGroupDesc() {
  const t = useT('users');
  return (g: Pick<GroupInfo, 'name' | 'description'>): string => (KNOWN_GROUPS.has(g.name) ? t(`groupDesc.${g.name}`) : g.description);
}
