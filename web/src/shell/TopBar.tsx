import { useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSession } from '../api';
import { useT } from '../i18n';
import { useTheme } from '../theme';
import { Icon, IconButton, Tooltip } from '../ui';
import { formatClock, relativeTime } from '../lib/format';
import { useI18n } from '../i18n';
import { clearNotices, markAllRead, useNotices } from './notifications';
import { Popover } from './Popover';

export function TopBar({ onSearch }: { onSearch(): void }) {
  const t = useT('shell');
  const { lang } = useI18n();
  const { host, isUnlocked, unlockLeft, unlockForever, lock } = useSession();
  const { isDark, toggleDark } = useTheme();
  const nav = useNavigate();
  const notices = useNotices();
  const unread = notices.filter((n) => !n.read).length;
  const bellRef = useRef<HTMLButtonElement>(null);
  const hostRef = useRef<HTMLButtonElement>(null);
  const [pop, setPop] = useState<null | { kind: 'bell' | 'host'; rect: DOMRect }>(null);

  // Unread notices keep their marker while the list is open; they become read when it closes.
  const close = () => {
    if (pop?.kind === 'bell') markAllRead();
    setPop(null);
  };
  const open = (kind: 'bell' | 'host', el: HTMLElement | null) => {
    if (!el) return;
    if (pop?.kind === kind) return close();
    if (pop?.kind === 'bell') markAllRead();
    setPop({ kind, rect: el.getBoundingClientRect() });
  };

  return (
    <header className="top">
      <button type="button" className="top-search" onClick={onSearch} aria-label={t('search.label')}>
        <Icon name="search" />
        <span>{t('search.placeholder')}</span>
        <kbd className="ui-kbd">Ctrl K</kbd>
      </button>
      <div className="top-sp" />
      {isUnlocked && (
        <Tooltip label={t('sudo.chipTip')}>
          <button type="button" className="sudo-chip" onClick={() => void lock()}>
            <Icon name="unlock" />
            {unlockForever ? t('sudo.chipForever') : t('sudo.chip', { time: formatClock(unlockLeft) })}
          </button>
        </Tooltip>
      )}
      <button ref={hostRef} type="button" className="top-pill" aria-haspopup="dialog" aria-expanded={pop?.kind === 'host'} onClick={() => open('host', hostRef.current)}>
        <i />
        {host?.hostname ?? t('host.thisMachine')}
        <Icon name="chevron" />
      </button>
      <IconButton
        className="top-ib"
        icon={isDark ? 'moon' : 'sun'}
        label={isDark ? t('theme.toLight') : t('theme.toDark')}
        variant="secondary"
        onClick={toggleDark}
      />
      <button ref={bellRef} type="button" className="top-ib" aria-label={unread ? t('notifications.unread', { count: unread }) : t('notifications.title')} aria-haspopup="dialog" aria-expanded={pop?.kind === 'bell'} onClick={() => open('bell', bellRef.current)}>
        <Icon name="bell" />
        {unread > 0 && <span className="dot" />}
      </button>

      {pop?.kind === 'bell' && (
        <Popover anchor={pop.rect} onClose={close} label={t('notifications.title')}>
          <div className="pop-hd">
            <span className="grow">{t('notifications.title')}</span>
            {notices.length > 0 && <button type="button" className="ui-toast-act" onClick={clearNotices}>{t('notifications.clear')}</button>}
          </div>
          {notices.length === 0 ? (
            <div className="pop-note">{t('notifications.empty')}</div>
          ) : (
            notices.map((n) => {
              const body = (
                <>
                  <span className="ic"><Icon name={n.icon ?? (n.tone === 'err' ? 'alert' : n.tone === 'ok' ? 'check' : 'info')} /></span>
                  <div className="note-tx">
                    <b>{n.title}</b>
                    {n.detail && <small className="note-detail">{n.detail}</small>}
                    <small>{relativeTime(n.at, lang)}</small>
                  </div>
                  {!n.read && <span className="note-unread" aria-hidden />}
                </>
              );
              return n.to ? (
                <button
                  key={n.id}
                  type="button"
                  className={`note note--link ${n.tone ?? ''}`}
                  onClick={() => {
                    setPop(null);
                    nav(n.to!);
                  }}
                >
                  {body}
                </button>
              ) : (
                <div key={n.id} className={`note ${n.tone ?? ''}`}>{body}</div>
              );
            })
          )}
        </Popover>
      )}
      {pop?.kind === 'host' && (
        <Popover anchor={pop.rect} onClose={() => setPop(null)} label={t('host.title')}>
          <div className="pop-hd"><span className="grow">{t('host.title')}</span></div>
          <div className="note">
            <span className="ic" style={{ background: 'var(--ok-s)', color: 'var(--ok)' }}><Icon name="server" /></span>
            <div>
              <b>{host?.hostname ?? t('host.thisMachine')}</b>
              <small>{t('host.thisMachine')}{host?.ip ? `, ${host.ip}` : ''}</small>
            </div>
          </div>
          <button
            type="button"
            className="ui-mi"
            onClick={() => {
              setPop(null);
              nav('/settings#hosts');
            }}
          >
            <Icon name="cog" />
            {t('host.manage')}
          </button>
        </Popover>
      )}
    </header>
  );
}
