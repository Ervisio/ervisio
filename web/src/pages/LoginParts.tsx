import { forwardRef } from 'react';
import { useSession, type RecentUser } from '../api';
import type { TFn } from '../i18n';
import type { Message } from './loginProblem';
import { BrandLockup, Icon, IconButton, Input } from '../ui';
import { DistroLogo } from '../shell/DistroLogo';

const TILE_HUES = ['usr', 'file', 'log'];

type Host = ReturnType<typeof useSession>['host'];

export function HostArt({ host }: { host: Host }) {
  return (
    <section className="lb-art" aria-label={host?.hostname}>
      <span className="glow" />
      <DistroLogo
        className="big"
        id={host?.distro.id}
        logo={host?.distro.logo}
        url={host?.distro.logoUrl}
      />
      <div className="nm">
        <h2>{host?.hostname ?? '...'}</h2>
        {host?.ip && <p className="ip">{host.ip}</p>}
        {host?.distro.name && <p className="os">{host.distro.name}</p>}
      </div>
    </section>
  );
}

type RecentAccountsProps = {
  t: TFn;
  recent: RecentUser[];
  user: string;
  other: boolean;
  onPick: (r: RecentUser) => void;
  onOther: () => void;
};

export function RecentAccounts({ t, recent, user, other, onPick, onOther }: RecentAccountsProps) {
  return (
    <div className="ugrid" role="group" aria-label={t('recent')}>
      {recent.map((r, i) => (
        <button
          key={r.user}
          type="button"
          className={`ut hue-${TILE_HUES[i % TILE_HUES.length]}`}
          aria-pressed={!other && user === r.user}
          onClick={() => onPick(r)}
        >
          <span className="av">{r.user.slice(0, 1)}</span>
          <b>{r.user}</b>
          <small>{r.isAdmin ? t('role.admin') : t('role.user')}</small>
        </button>
      ))}
      <button type="button" className="ut other" aria-pressed={other} onClick={onOther}>
        <span className="av">
          <Icon name="plus" />
        </span>
        <b>{t('other')}</b>
        <small>{t('otherHint')}</small>
      </button>
    </div>
  );
}

export type Method = 'password' | 'key';

export function MethodSwitch({
  t,
  keyMode,
  busy,
  onSwitch,
}: {
  t: TFn;
  keyMode: boolean;
  busy: boolean;
  onSwitch: (m: Method) => void;
}) {
  return (
    <>
      <div className="sk-or" aria-hidden="true">
        {t('or')}
      </div>
      {keyMode ? (
        <button
          type="button"
          className="sk-alt"
          onClick={() => onSwitch('password')}
          disabled={busy}
        >
          <Icon name="shield" />
          {t('usePassword')}
        </button>
      ) : (
        <button type="button" className="sk-alt" onClick={() => onSwitch('key')}>
          <Icon name="key" />
          {t('useKey')}
        </button>
      )}
    </>
  );
}

type UsernameFieldProps = { t: TFn; value: string; onChange: (value: string) => void };

export const UsernameField = forwardRef<HTMLInputElement, UsernameFieldProps>(
  function UsernameField({ t, value, onChange }, ref) {
    return (
      <Input
        ref={ref}
        label={t('username')}
        icon="user"
        id="u"
        name="username"
        autoComplete="username"
        autoCapitalize="none"
        spellCheck={false}
        placeholder={t('usernamePlaceholder')}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  },
);

type PasswordFieldProps = {
  t: TFn;
  value: string;
  onChange: (value: string) => void;
  isWrong: boolean;
  isShown: boolean;
  onToggleShown: () => void;
};

export const PasswordField = forwardRef<HTMLInputElement, PasswordFieldProps>(
  function PasswordField({ t, value, onChange, isWrong, isShown, onToggleShown }, ref) {
    return (
      <Input
        ref={ref}
        label={t('password')}
        icon="shield"
        id="p"
        name="password"
        type={isShown ? 'text' : 'password'}
        autoComplete="current-password"
        placeholder={t('passwordPlaceholder')}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        error={isWrong ? true : undefined}
        end={
          <IconButton
            icon={isShown ? 'eyeoff' : 'eye'}
            label={isShown ? t('hidePassword') : t('showPassword')}
            onClick={onToggleShown}
          />
        }
      />
    );
  },
);

export function LoginAlert({ message }: { message: Message | null }) {
  if (!message) return null;
  return (
    <div className={`lmsg ${message.hue}`} role="alert">
      <Icon name={message.icon} />
      <div>{message.body}</div>
    </div>
  );
}

export function KeyNote({ t }: { t: TFn }) {
  return (
    <p className="sk-note">
      <Icon name="shield" />
      <span>{t('key.note')}</span>
    </p>
  );
}

export function LoginFooter({
  t,
  isDark,
  onToggleDark,
}: {
  t: TFn;
  isDark: boolean;
  onToggleDark: () => void;
}) {
  return (
    <div className="lb-foot">
      <BrandLockup size={20} />
      <button type="button" onClick={onToggleDark}>
        <Icon name={isDark ? 'moon' : 'sun'} size={15} />
        {t('theme')}
      </button>
    </div>
  );
}
