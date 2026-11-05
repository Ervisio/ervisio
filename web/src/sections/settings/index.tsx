import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation } from 'react-router-dom';
import { recentUsersEnabled, setRecentUsersEnabled, useSession, usePrefs } from '../../api';
import { FormerName, Name } from '../../brand';
import { LANGUAGES, useI18n, useT, type Lang, type TFn } from '../../i18n';
import { useTheme, type ThemeValue } from '../../theme';
import {
  BrandLockup,
  Button,
  Icon,
  Input,
  Segmented,
  Select,
  Switch,
  toast,
  type HueId,
  type IconName,
} from '../../ui';
import { RefreshSelect } from '../../lib/RefreshSelect';
import { REFRESH_KEY } from '../../lib/refresh';
import { ActivityLog } from './ActivityLog';
import { ColourBlock, ThemeGrid } from './Appearance';
import { ChannelsBlock } from './Channels';
import { EnvironmentsBlock } from './Environments';
import { HostsBlock, type HostEntry } from './Hosts';
import { NetworkHostsBlock } from './NetworkHosts';
import { PairingBlock } from './Pairing';
import type { EnvList } from './envs';
import { NameList } from './NameList';
import { PluginJobsBlock } from './PluginJobs';
import { usePrefSave, useServerConfig, type CfgState } from './save';
import { UpdatesBlock } from './Updates';
import { updateStatus } from './updatesApi';
import './settings.css';

interface RowDef {
  id: string;
  title: string;
  desc?: string;
  /** Config key shown in mono under the title (server settings). */
  cfgKey?: string;
  control?: ReactNode;
  /** Full-width content under the title instead of a control on the right. */
  block?: ReactNode;
  /** Extra words for the search. */
  words?: string;
  /** The daemon must restart for this key to apply. */
  restart?: boolean;
}
interface GroupDef {
  id: string;
  part: 'you' | 'server' | 'about';
  icon: IconName;
  hue: HueId;
  title: string;
  admin?: boolean;
  rows: RowDef[];
  /** Shown above the rows. */
  top?: ReactNode;
}

const DURATIONS = ['1h', '4h', '12h', '24h', '168h'];
const toGoDur = (v: string) => v;

type SessionValue = ReturnType<typeof useSession>;
type Session = SessionValue['session'];
type ServerConfig = ReturnType<typeof useServerConfig>;
type Save = ReturnType<typeof usePrefSave>;
type Region = { timeFormat: '24' | '12'; weekStart: string };
type TermPrefs = { theme: string; fontSize: number; copyOnSelect: boolean; snippets: boolean };
type Patch = Record<string, unknown>;
/** What the server-config rows need: the config, its setter and whether it can be edited yet. */
interface ServerRows {
  t: TFn;
  server: ServerConfig;
  disabled: boolean;
  set: ServerConfig['set'];
}

export default function SettingsPage() {
  const t = useT('settings');
  const te = useT('envs');
  const { session, host, isUnlocked } = useSession();
  const { prefs } = usePrefs();
  const th = useTheme();
  const { lang, setLang } = useI18n();
  const save = usePrefSave();
  const loc = useLocation();
  const isAdmin = !!session && (session.isAdmin || session.canSudo || !!session.isRoot);
  // What the daemon shows in the activity log: everyone's entries to root and to an unlocked administrator.
  const canSeeAll = !!session && (!!session.isRoot || isUnlocked);
  const server = useServerConfig(isAdmin);
  const managedBy = useManagedBy(isAdmin);
  const [q, setQ] = useState('');
  // Environments: the list loads itself; the pairings come with it. Bumping the revision reloads.
  const [envData, setEnvData] = useState<EnvList | null>(null);
  const [envRev, setEnvRev] = useState(0);
  const [recentOn, setRecentOn] = useState(recentUsersEnabled);
  const setRecent = (on: boolean) => {
    setRecentUsersEnabled(on);
    setRecentOn(on);
    toast.info(t('saved', { name: t('browser.recent.title') }));
  };
  const { scrollRef, active, onScroll, go } = useSettingsScroll(loc.hash, server.state);

  const region = { timeFormat: '24', weekStart: 'mon', ...(prefs.region ?? {}) } as Region;
  const term = {
    theme: 'app',
    fontSize: 14,
    copyOnSelect: true,
    snippets: true,
    ...(prefs.terminal ?? {}),
  } as TermPrefs;
  const notify = {
    serviceFail: true,
    updates: true,
    sshFail: true,
    browser: false,
    ...(prefs.notify ?? {}),
  } as Record<string, boolean>;
  const setTerm = (patch: Patch) => save('terminal', { ...term, ...patch }, t('groups.terminal'));
  const setRegion = (patch: Patch) => save('region', { ...region, ...patch }, t('groups.language'));
  const setNotify = (key: string, v: boolean, name: string) => {
    if (
      key === 'browser' &&
      v &&
      typeof Notification !== 'undefined' &&
      Notification.permission === 'default'
    ) {
      void Notification.requestPermission().then((p) => {
        if (p !== 'granted') toast.info(t('notify.browserDenied'));
        save('notify', { ...notify, [key]: p === 'granted' }, name);
      });
      return;
    }
    save('notify', { ...notify, [key]: v }, name);
  };

  const srv: ServerRows = { t, server, disabled: server.state !== 'ready', set: server.set };
  const groups: GroupDef[] = [
    appearanceGroup(t, th, save),
    languageGroup(t, lang, setLang, region, setRegion),
    terminalGroup(t, term, setTerm),
    notificationsGroup(t, notify, setNotify),
    browserGroup(t, recentOn, setRecent),
    activityGroup(srv, canSeeAll, session, isAdmin),
  ];
  if (isAdmin) {
    groups.push(
      signinGroup(srv, serverNotice(t, server, isUnlocked), go),
      webGroup(srv),
      pluginPolicyGroup(srv, te),
      ...daemonGroups(t),
      envsGroup(srv, te, envData, setEnvData, envRev, () => setEnvRev((n) => n + 1)),
      hostsGroup(srv),
    );
  }
  groups.push(aboutGroup(srv, isAdmin, managedBy, session, host));

  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const visible = filterGroups(groups, words);

  return (
    <div className="st hue-set">
      <SettingsNav t={t} groups={groups} active={active} go={go} />
      <div className="st-scroll" ref={scrollRef} onScroll={onScroll}>
        <div className="st-inner">
          <Input
            fieldClassName="st-search"
            icon="search"
            aria-label={t('search.placeholder')}
            placeholder={t('search.placeholder')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          {visible.length === 0 && <div className="st-empty">{t('search.empty')}</div>}
          {visible.map(({ g, rows }, i) => (
            <section key={g.id} data-gid={g.id} aria-labelledby={`st-${g.id}`}>
              {!words.length && (i === 0 || visible[i - 1].g.part !== g.part) && (
                <div className="st-part">
                  {g.part === 'server' ? t('parts.serverAdmins') : partTitle(t, g.part)}
                </div>
              )}
              <h3 className="st-gt" id={`st-${g.id}`}>
                <span className={`st-tile hue-${g.hue}`}>
                  <Icon name={g.icon} />
                </span>
                {g.title}
                {g.admin && (
                  <span className="st-lockb">
                    <Icon name="lock" />
                    {t('adminOnly')}
                  </span>
                )}
              </h3>
              <div
                className={`st-grp${g.admin && server.state !== 'ready' ? ' st-grp--locked' : ''}`}
              >
                {g.top}
                {rows.map((r) => settingsRow(t, r))}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}

/** A packaged install (.deb, .rpm, AUR) is updated by its package manager: no automatic install here. */
function useManagedBy(isAdmin: boolean) {
  const [managedBy, setManagedBy] = useState('');
  useEffect(() => {
    if (!isAdmin) return;
    let live = true;
    updateStatus().then(
      (s) => live && setManagedBy(s.managedBy ?? ''),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [isAdmin]);
  return managedBy;
}

/** Scroll spy over the group sections and deep links to a group by #hash. */
function useSettingsScroll(hash: string, serverState: CfgState) {
  const [active, setActive] = useState('appearance');
  const scrollRef = useRef<HTMLDivElement>(null);
  const onScroll = useCallback(() => {
    const sc = scrollRef.current;
    if (!sc) return;
    let cur = '';
    sc.querySelectorAll<HTMLElement>('[data-gid]').forEach((el) => {
      if (el.offsetTop - sc.offsetTop - 40 <= sc.scrollTop) cur = el.dataset.gid!;
    });
    if (cur) setActive(cur);
  }, []);
  const go = (id: string) => {
    const el = scrollRef.current?.querySelector<HTMLElement>(`[data-gid="${id}"]`);
    if (el && scrollRef.current)
      scrollRef.current.scrollTo({ top: el.offsetTop - scrollRef.current.offsetTop - 10 });
    setActive(id);
  };
  useEffect(() => {
    const id = hash.replace('#', '');
    if (id) window.setTimeout(() => go(id), 50);
  }, [hash, serverState]);
  return { scrollRef, active, onScroll, go };
}

function filterGroups(groups: GroupDef[], words: string[]) {
  const gHit = (str: string) => words.every((w) => str.toLowerCase().includes(w));
  return groups
    .map((g) => ({
      g,
      rows: words.length
        ? g.rows.filter((r) =>
            gHit(`${g.title} ${r.title} ${r.desc ?? ''} ${r.cfgKey ?? ''} ${r.words ?? ''}`),
          )
        : g.rows,
    }))
    .filter((x) => x.rows.length > 0);
}

const partTitle = (t: TFn, p: GroupDef['part']) =>
  p === 'you' ? t('parts.you') : p === 'server' ? t('parts.server') : Name;

function SettingsNav({
  t,
  groups,
  active,
  go,
}: {
  t: TFn;
  groups: GroupDef[];
  active: string;
  go(id: string): void;
}) {
  const navGroups = (part: GroupDef['part']) => groups.filter((g) => g.part === part);
  return (
    <nav className="st-nav" aria-label={t('title')}>
      <h2>{t('title')}</h2>
      {(['you', 'server', 'about'] as const).map((p) =>
        navGroups(p).length ? (
          <div key={p} style={{ display: 'contents' }}>
            <div className="st-gl">{partTitle(t, p)}</div>
            {navGroups(p).map((g) => (
              <button
                key={g.id}
                type="button"
                className="st-ni"
                aria-current={active === g.id}
                onClick={() => go(g.id)}
              >
                <span className={`st-tile hue-${g.hue}`}>
                  <Icon name={g.icon} />
                </span>
                {g.title}
                {g.admin && <Icon name="lock" />}
              </button>
            ))}
          </div>
        ) : null,
      )}
    </nav>
  );
}

function rowText(t: TFn, r: RowDef) {
  return (
    <>
      <b>{r.title}</b>
      {r.desc && <small>{r.desc}</small>}
      {r.cfgKey && (
        <span className="st-key">
          {r.cfgKey}
          {r.restart ? ` · ${t('server.restart')}` : ''}
        </span>
      )}
    </>
  );
}

function settingsRow(t: TFn, r: RowDef) {
  if (r.block) {
    return (
      <div className="st-row st-row--block" key={r.id}>
        <div className="st-tx" style={{ marginBottom: 12 }}>
          {rowText(t, r)}
        </div>
        {r.block}
      </div>
    );
  }
  return (
    <div className="st-row" key={r.id}>
      <div className="st-tx">{rowText(t, r)}</div>
      {r.control && <div className="st-ctl">{r.control}</div>}
    </div>
  );
}

function appearanceGroup(t: TFn, th: ThemeValue, save: Save): GroupDef {
  const themeOptions = (kind: 'dark' | 'light') =>
    th.themes.filter((x) => x.kind === kind).map((x) => ({ value: x.id, label: x.name }));
  return {
    id: 'appearance',
    part: 'you',
    icon: 'palette',
    hue: 'plg',
    title: t('groups.appearance'),
    rows: [
      {
        id: 'theme',
        title: t('appearance.theme.title'),
        desc: t('appearance.theme.desc'),
        words: 'dark light oled midnight',
        block: <ThemeGrid />,
      },
      {
        id: 'colours',
        title: t('appearance.colours.title'),
        desc: t('appearance.colours.desc'),
        words: 'distro monochrome colour',
        block: <ColourBlock />,
      },
      {
        id: 'follow',
        title: t('appearance.follow.title'),
        desc: t('appearance.follow.desc'),
        control: (
          <Switch
            aria-label={t('appearance.follow.title')}
            checked={th.follow.on}
            onChange={(on) => {
              const s = th.snapshot();
              th.setFollow({ ...th.follow, on });
              toast.undo(t('saved', { name: t('appearance.follow.title') }), t('undo'), () =>
                th.restore(s),
              );
            }}
          />
        ),
      },
      ...(th.follow.on
        ? [
            {
              id: 'follow-pair',
              title: t('appearance.follow.pair'),
              control: (
                <>
                  <Select
                    compact
                    aria-label={t('appearance.dark')}
                    value={th.follow.dark}
                    options={themeOptions('dark')}
                    onChange={(v) => th.setFollow({ ...th.follow, dark: v })}
                  />
                  <Select
                    compact
                    aria-label={t('appearance.light')}
                    value={th.follow.light}
                    options={themeOptions('light')}
                    onChange={(v) => th.setFollow({ ...th.follow, light: v })}
                  />
                </>
              ),
            },
          ]
        : []),
      {
        id: 'density',
        title: t('appearance.density.title'),
        desc: t('appearance.density.desc'),
        control: (
          <Segmented
            aria-label={t('appearance.density.title')}
            value={th.density}
            onChange={(d) => {
              th.setDensity(d);
              toast.undo(t('saved', { name: t('appearance.density.title') }), t('undo'), () =>
                th.setDensity(th.density),
              );
            }}
            options={[
              { value: 'comfortable', label: t('appearance.density.comfortable') },
              { value: 'compact', label: t('appearance.density.compact') },
            ]}
          />
        ),
      },
      {
        id: 'motion',
        title: t('appearance.motion.title'),
        desc: t('appearance.motion.desc'),
        control: (
          <Switch
            aria-label={t('appearance.motion.title')}
            checked={th.reduceMotion}
            onChange={(on) => {
              th.setReduceMotion(on);
              toast.undo(t('saved', { name: t('appearance.motion.title') }), t('undo'), () =>
                th.setReduceMotion(!on),
              );
            }}
          />
        ),
      },
      {
        id: 'refresh',
        title: t('appearance.refresh.title'),
        desc: t('appearance.refresh.desc'),
        words: 'refresh interval update polling live seconds rate aggiorna frequenza',
        control: (
          <RefreshSelect
            label={t('appearance.refresh.title')}
            onPick={(ms) => save(REFRESH_KEY, ms, t('appearance.refresh.title'))}
          />
        ),
      },
    ],
  };
}

function languageGroup(
  t: TFn,
  lang: Lang,
  setLang: (l: Lang) => void,
  region: Region,
  setRegion: (patch: Patch) => void,
): GroupDef {
  return {
    id: 'language',
    part: 'you',
    icon: 'globe',
    hue: 'ov',
    title: t('groups.language'),
    rows: [
      {
        id: 'lang',
        title: t('language.title'),
        desc: t('language.desc'),
        control: (
          <Select
            compact
            aria-label={t('language.title')}
            value={lang}
            options={LANGUAGES.map((l) => ({ value: l.id, label: l.name }))}
            onChange={(v) => {
              const before = lang;
              setLang(v as Lang);
              toast.undo(t('saved', { name: t('language.title') }), t('undo'), () =>
                setLang(before),
              );
            }}
          />
        ),
      },
      {
        id: 'timefmt',
        title: t('language.time'),
        control: (
          <Segmented
            aria-label={t('language.time')}
            value={region.timeFormat}
            onChange={(v) => setRegion({ timeFormat: v })}
            options={[
              { value: '24', label: t('language.h24') },
              { value: '12', label: t('language.h12') },
            ]}
          />
        ),
      },
      {
        id: 'week',
        title: t('language.week'),
        control: (
          <Select
            compact
            aria-label={t('language.week')}
            value={region.weekStart}
            onChange={(v) => setRegion({ weekStart: v })}
            options={[
              { value: 'mon', label: t('language.mon') },
              { value: 'sun', label: t('language.sun') },
              { value: 'sat', label: t('language.sat') },
            ]}
          />
        ),
      },
    ],
  };
}

function terminalGroup(t: TFn, term: TermPrefs, setTerm: (patch: Patch) => void): GroupDef {
  return {
    id: 'terminal',
    part: 'you',
    icon: 'terminal',
    hue: 'term',
    title: t('groups.terminal'),
    rows: [
      {
        id: 'tcolour',
        title: t('terminal.colour'),
        desc: t('terminal.colourDesc'),
        control: (
          <Select
            compact
            aria-label={t('terminal.colour')}
            value={term.theme}
            onChange={(v) => setTerm({ theme: v })}
            options={[
              { value: 'app', label: t('terminal.followApp') },
              { value: 'dark', label: t('appearance.dark') },
              { value: 'light', label: t('appearance.light') },
            ]}
          />
        ),
      },
      {
        id: 'tfont',
        title: t('terminal.font'),
        control: (
          <Segmented
            aria-label={t('terminal.font')}
            value={String(term.fontSize)}
            onChange={(v) => setTerm({ fontSize: Number(v) })}
            options={['12', '13', '14', '16'].map((n) => ({ value: n, label: n }))}
          />
        ),
      },
      {
        id: 'tcopy',
        title: t('terminal.copy'),
        control: (
          <Switch
            aria-label={t('terminal.copy')}
            checked={term.copyOnSelect}
            onChange={(v) => setTerm({ copyOnSelect: v })}
          />
        ),
      },
      {
        id: 'tsnip',
        title: t('terminal.snippets'),
        desc: t('terminal.snippetsDesc'),
        control: (
          <Switch
            aria-label={t('terminal.snippets')}
            checked={term.snippets}
            onChange={(v) => setTerm({ snippets: v })}
          />
        ),
      },
    ],
  };
}

function notificationsGroup(
  t: TFn,
  notify: Record<string, boolean>,
  setNotify: (key: string, v: boolean, name: string) => void,
): GroupDef {
  return {
    id: 'notifications',
    part: 'you',
    icon: 'bell',
    hue: 'log',
    title: t('groups.notifications'),
    rows: [
      {
        id: 'nsvc',
        title: t('notify.service'),
        control: (
          <Switch
            aria-label={t('notify.service')}
            checked={notify.serviceFail}
            onChange={(v) => setNotify('serviceFail', v, t('notify.service'))}
          />
        ),
      },
      {
        id: 'nupd',
        title: t('notify.updates'),
        control: (
          <Switch
            aria-label={t('notify.updates')}
            checked={notify.updates}
            onChange={(v) => setNotify('updates', v, t('notify.updates'))}
          />
        ),
      },
      {
        id: 'nssh',
        title: t('notify.ssh'),
        desc: t('notify.sshDesc'),
        control: (
          <Switch
            aria-label={t('notify.ssh')}
            checked={notify.sshFail}
            onChange={(v) => setNotify('sshFail', v, t('notify.ssh'))}
          />
        ),
      },
      {
        id: 'nbrowser',
        title: t('notify.browser'),
        desc: t('notify.browserDesc', { name: Name }),
        control: (
          <Switch
            aria-label={t('notify.browser')}
            checked={notify.browser}
            onChange={(v) => setNotify('browser', v, t('notify.browser'))}
          />
        ),
      },
    ],
  };
}

function browserGroup(t: TFn, recentOn: boolean, setRecent: (on: boolean) => void): GroupDef {
  return {
    id: 'browser',
    part: 'you',
    icon: 'shield',
    hue: 'usr',
    title: t('groups.browser'),
    rows: [
      {
        id: 'recentusers',
        title: t('browser.recent.title'),
        desc: t('browser.recent.desc'),
        words: 'sign-in login accounts names privacy shared',
        control: (
          <Switch aria-label={t('browser.recent.title')} checked={recentOn} onChange={setRecent} />
        ),
      },
    ],
  };
}

function activityGroup(
  { t, server, disabled: srvDisabled, set: sv }: ServerRows,
  canSeeAll: boolean,
  session: Session,
  isAdmin: boolean,
): GroupDef {
  return {
    id: 'activity',
    part: 'you',
    icon: 'clock',
    hue: 'log',
    title: t('groups.activity'),
    rows: [
      {
        id: 'activitylog',
        title: t('activity.title'),
        desc: t('activity.desc'),
        words: 'audit history who did what log export csv json plugins sign-in cronologia attivita',
        block: <ActivityLog canSeeAll={canSeeAll} canUnlock={!!session?.canSudo} />,
      },
      ...(isAdmin
        ? [
            {
              id: 'auditon',
              title: t('activity.on.title'),
              desc: t('activity.on.desc'),
              cfgKey: 'audit.enabled = ' + String(server.get('audit.enabled', true)),
              control: (
                <Switch
                  aria-label={t('activity.on.title')}
                  disabled={srvDisabled}
                  checked={server.get('audit.enabled', true)}
                  onChange={(v) => void sv('audit.enabled', v, t('activity.on.title'))}
                />
              ),
            },
            {
              id: 'auditkeep',
              title: t('activity.keep.title'),
              desc: t('activity.keep.desc'),
              cfgKey: `audit.retention_days = ${server.get('audit.retention_days', 90)}`,
              words: 'retention days delete old',
              control: (
                <Select
                  compact
                  aria-label={t('activity.keep.title')}
                  disabled={srvDisabled}
                  value={String(server.get('audit.retention_days', 90))}
                  options={withCurrent(
                    ['30', '90', '180', '365', '0'].map((n) => ({
                      value: n,
                      label:
                        n === '0'
                          ? t('activity.keep.forever')
                          : t('activity.keep.days', { count: Number(n) }),
                    })),
                    String(server.get('audit.retention_days', 90)),
                  )}
                  onChange={(v) =>
                    void sv('audit.retention_days', Number(v), t('activity.keep.title'))
                  }
                />
              ),
            },
          ]
        : []),
    ],
  };
}

function serverNotice(t: TFn, server: ServerConfig, isUnlocked: boolean) {
  return (
    <>
      {server.state === 'failed' && (
        <div className="st-lock" role="alert">
          <Icon name="alert" size={22} />
          <div className="grow">
            <b>{t('server.failed.title')}</b>
            <small>{t('server.failed.text')}</small>
          </div>
          <Button icon="refresh" onClick={() => void server.reload()}>
            {t('server.failed.action')}
          </Button>
        </div>
      )}
      <div className="st-warn">
        <Icon name="alert" />
        <div>
          {t('server.warn')}
          {isUnlocked ? '' : ' ' + t('server.askPassword')}
        </div>
      </div>
      {server.warnings.length > 0 && (
        <div className="st-warn">
          <Icon name="info" />
          <div>{t('server.unknownKeys', { keys: server.warnings.join(', ') })}</div>
        </div>
      )}
    </>
  );
}

function signinGroup(
  { t, server, disabled: srvDisabled, set: sv }: ServerRows,
  top: ReactNode,
  go: (id: string) => void,
): GroupDef {
  const durOptions = DURATIONS.map((d) => ({ value: d, label: t(`dur.${d}`) }));
  const unlockOptions = ['1m', '5m', '15m', '30m', '1h', '4h', '0s'].map((d) => ({
    value: d,
    label: t(`dur.${d}`),
  }));
  return {
    id: 'signin',
    part: 'server',
    icon: 'key',
    hue: 'svc',
    title: t('groups.signin'),
    admin: true,
    top,
    rows: [
      {
        id: 'root',
        title: t('signin.root'),
        desc: t('signin.rootDesc'),
        cfgKey: 'allow_root = ' + String(server.get('allow_root', false)),
        control: (
          <Switch
            aria-label={t('signin.root')}
            disabled={srvDisabled}
            checked={server.get('allow_root', false)}
            onChange={(v) => void sv('allow_root', v, t('signin.root'))}
          />
        ),
      },
      {
        id: 'adminsonly',
        title: t('signin.adminsOnly'),
        desc: t('signin.adminsOnlyDesc'),
        cfgKey: 'auth.admins_only = ' + String(server.get('auth.admins_only', false)),
        words: 'who may sign in allow administrators sudo wheel',
        control: (
          <Switch
            aria-label={t('signin.adminsOnly')}
            disabled={srvDisabled}
            checked={server.get('auth.admins_only', false)}
            onChange={(v) => void sv('auth.admins_only', v, t('signin.adminsOnly'))}
          />
        ),
      },
      {
        id: 'allowusers',
        title: t('signin.allowUsers'),
        desc: t('signin.allowUsersDesc'),
        cfgKey: `auth.allow_users = [${server
          .get<string[]>('auth.allow_users', [])
          .map((n) => `"${n}"`)
          .join(', ')}]`,
        words: 'who may sign in allow users accounts',
        block: (
          <NameList
            names={server.get<string[]>('auth.allow_users', [])}
            disabled={srvDisabled}
            label={t('signin.allowUsers')}
            placeholder={t('signin.allowUsersHint')}
            onChange={(next) => void sv('auth.allow_users', next, t('signin.allowUsers'))}
          />
        ),
      },
      {
        id: 'allowgroups',
        title: t('signin.allowGroups'),
        desc: t('signin.allowGroupsDesc'),
        cfgKey: `auth.allow_groups = [${server
          .get<string[]>('auth.allow_groups', [])
          .map((n) => `"${n}"`)
          .join(', ')}]`,
        words: 'who may sign in allow groups',
        block: (
          <NameList
            names={server.get<string[]>('auth.allow_groups', [])}
            disabled={srvDisabled}
            label={t('signin.allowGroups')}
            placeholder={t('signin.allowGroupsHint')}
            onChange={(next) => void sv('auth.allow_groups', next, t('signin.allowGroups'))}
          />
        ),
      },
      {
        id: 'showip',
        title: t('signin.showIp'),
        desc: t('signin.showIpDesc'),
        cfgKey: 'login.show_ip = ' + String(server.get('login.show_ip', true)),
        control: (
          <Switch
            aria-label={t('signin.showIp')}
            disabled={srvDisabled}
            checked={server.get('login.show_ip', true)}
            onChange={(v) => void sv('login.show_ip', v, t('signin.showIp'))}
          />
        ),
      },
      {
        id: 'timeout',
        title: t('signin.timeout'),
        cfgKey: `session.timeout = "${server.get('session.timeout', '12h')}"`,
        control: (
          <Select
            compact
            aria-label={t('signin.timeout')}
            disabled={srvDisabled}
            value={normDur(server.get('session.timeout', '12h'))}
            options={withCurrent(durOptions, server.get('session.timeout', '12h'))}
            onChange={(v) => void sv('session.timeout', toGoDur(v), t('signin.timeout'))}
          />
        ),
      },
      {
        id: 'unlock',
        title: t('signin.unlock'),
        desc: t('signin.unlockDesc'),
        cfgKey: `session.admin_unlock = "${server.get('session.admin_unlock', '5m')}"`,
        control: (
          <Select
            compact
            aria-label={t('signin.unlock')}
            disabled={srvDisabled}
            value={server.get('session.admin_unlock', '5m')}
            options={withCurrent(unlockOptions, server.get('session.admin_unlock', '5m'))}
            onChange={(v) => void sv('session.admin_unlock', v, t('signin.unlock'))}
          />
        ),
      },
      {
        id: 'activitylink',
        title: t('signin.activity'),
        desc: t('signin.activityDesc'),
        words: 'audit history who did what',
        control: (
          <Button icon="clock" onClick={() => go('activity')}>
            {t('signin.activityOpen')}
          </Button>
        ),
      },
      {
        id: 'failures',
        title: t('signin.failures'),
        desc: t('signin.failuresDesc'),
        cfgKey: `login.max_failures = ${server.get('login.max_failures', 5)}`,
        control: (
          <Select
            compact
            aria-label={t('signin.failures')}
            disabled={srvDisabled}
            value={String(server.get('login.max_failures', 5))}
            options={withCurrent(
              ['3', '5', '10', '20'].map((n) => ({
                value: n,
                label: t('signin.attempts', { count: Number(n) }),
              })),
              String(server.get('login.max_failures', 5)),
            )}
            onChange={(v) => void sv('login.max_failures', Number(v), t('signin.failures'))}
          />
        ),
      },
    ],
  };
}

function webGroup({ t, server, disabled: srvDisabled, set: sv }: ServerRows): GroupDef {
  return {
    id: 'web',
    part: 'server',
    icon: 'net',
    hue: 'file',
    title: t('groups.web'),
    admin: true,
    rows: [
      {
        id: 'listen',
        title: t('web.listen'),
        restart: server.needsRestart('listen'),
        cfgKey: `listen = "${server.get('listen', '0.0.0.0:9090')}"`,
        control: (
          <CommitInput
            disabled={srvDisabled}
            value={server.get('listen', '0.0.0.0:9090')}
            label={t('web.listen')}
            onCommit={(v) => void sv('listen', v, t('web.listen'))}
          />
        ),
      },
      {
        id: 'tls',
        title: t('web.tls'),
        desc: t('web.tlsDesc'),
        restart: server.needsRestart('tls.mode'),
        cfgKey: `tls.mode = "${server.get('tls.mode', 'self-signed')}"`,
        control: (
          <Segmented
            aria-label={t('web.tls')}
            value={server.get('tls.mode', 'self-signed')}
            onChange={(v) => (srvDisabled ? undefined : void sv('tls.mode', v, t('web.tls')))}
            options={[
              { value: 'self-signed', label: t('web.selfSigned') },
              { value: 'letsencrypt', label: t('web.letsencrypt') },
              { value: 'custom', label: t('web.custom') },
              { value: 'http', label: t('web.http') },
            ]}
          />
        ),
      },
      ...(server.get<string>('tls.mode', 'self-signed') === 'http'
        ? []
        : [
            {
              id: 'redirect',
              title: t('web.redirect'),
              restart: server.needsRestart('tls.redirect'),
              cfgKey: 'tls.redirect = ' + String(server.get('tls.redirect', true)),
              control: (
                <Switch
                  aria-label={t('web.redirect')}
                  disabled={srvDisabled}
                  checked={server.get('tls.redirect', true)}
                  onChange={(v) => void sv('tls.redirect', v, t('web.redirect'))}
                />
              ),
            },
          ]),
      ...(server.get<string>('tls.mode', 'self-signed') === 'custom'
        ? [
            {
              id: 'cert',
              title: t('web.cert'),
              desc: t('web.certDesc'),
              restart: true,
              cfgKey: `tls.cert = "${server.get('tls.cert', '')}"`,
              control: (
                <CommitInput
                  disabled={srvDisabled}
                  allowEmpty
                  value={server.get('tls.cert', '')}
                  label={t('web.cert')}
                  onCommit={(v) => void sv('tls.cert', v, t('web.cert'))}
                />
              ),
            },
            {
              id: 'key',
              title: t('web.key'),
              restart: true,
              cfgKey: `tls.key = "${server.get('tls.key', '')}"`,
              control: (
                <CommitInput
                  disabled={srvDisabled}
                  allowEmpty
                  value={server.get('tls.key', '')}
                  label={t('web.key')}
                  onCommit={(v) => void sv('tls.key', v, t('web.key'))}
                />
              ),
            },
          ]
        : []),
    ],
  };
}

function pluginPolicyGroup(
  { t, server, disabled: srvDisabled, set: sv }: ServerRows,
  te: TFn,
): GroupDef {
  return {
    id: 'plugpol',
    part: 'server',
    icon: 'plugins',
    hue: 'plg',
    title: t('groups.plugpol'),
    admin: true,
    rows: [
      {
        id: 'unsigned',
        title: t('plugpol.unsigned'),
        desc: t('plugpol.unsignedDesc', { name: Name }),
        cfgKey: 'plugins.allow_unsigned = ' + String(server.get('plugins.allow_unsigned', false)),
        control: (
          <Switch
            aria-label={t('plugpol.unsigned')}
            disabled={srvDisabled}
            checked={server.get('plugins.allow_unsigned', false)}
            onChange={(v) => void sv('plugins.allow_unsigned', v, t('plugpol.unsigned'))}
          />
        ),
      },
      {
        id: 'dev',
        title: t('plugpol.dev'),
        desc: t('plugpol.devDesc'),
        cfgKey: 'plugins.dev = ' + String(server.get('plugins.dev', false)),
        control: (
          <Switch
            aria-label={t('plugpol.dev')}
            disabled={srvDisabled}
            checked={server.get('plugins.dev', false)}
            onChange={(v) => void sv('plugins.dev', v, t('plugpol.dev'))}
          />
        ),
      },
      {
        id: 'netHosts',
        title: te('hosts.title'),
        desc: te('hosts.desc'),
        words: 'network hosts approved registry allow rete host approvati',
        block: <NetworkHostsBlock disabled={srvDisabled} />,
      },
      {
        id: 'catalog',
        title: t('plugpol.catalog'),
        desc: t('plugpol.catalogDesc'),
        cfgKey: `plugins.catalog_url = "${server.get('plugins.catalog_url', '')}"`,
        control: (
          <CommitInput
            disabled={srvDisabled}
            allowEmpty
            value={server.get('plugins.catalog_url', '')}
            label={t('plugpol.catalog')}
            onCommit={(v) => void sv('plugins.catalog_url', v, t('plugpol.catalog'))}
          />
        ),
      },
    ],
  };
}

/** Daemon-owned admin state that manages itself: notification channels and plugin jobs. */
function daemonGroups(t: TFn): GroupDef[] {
  return [
    {
      id: 'channels',
      part: 'server',
      icon: 'bell',
      hue: 'log',
      title: t('groups.channels'),
      admin: true,
      rows: [
        {
          id: 'channellist',
          title: t('channels.title'),
          words:
            'notifications alerts email smtp telegram webhook ntfy gotify slack discord notifiche avvisi',
          block: <ChannelsBlock />,
        },
      ],
    },
    {
      id: 'pluginjobs',
      part: 'server',
      icon: 'clock',
      hue: 'plg',
      title: t('groups.pluginjobs'),
      admin: true,
      rows: [
        {
          id: 'joblist',
          title: t('jobs.title'),
          words: 'background schedule cron webhook automation plugins jobs attività pianificate',
          block: <PluginJobsBlock />,
        },
      ],
    },
  ];
}

function envsGroup(
  { disabled: srvDisabled }: ServerRows,
  te: TFn,
  envData: EnvList | null,
  onData: (data: EnvList | null) => void,
  envRev: number,
  reload: () => void,
): GroupDef {
  return {
    id: 'envs',
    part: 'server',
    icon: 'link',
    hue: 'term',
    title: te('group'),
    admin: true,
    rows: [
      {
        id: 'envlist',
        title: te('list.title'),
        desc: te('list.desc'),
        words:
          'environments docker remote host tcp tls ssh portainer agent ervisio endpoint ambienti',
        block: <EnvironmentsBlock key={envRev} disabled={srvDisabled} onData={onData} />,
      },
      {
        id: 'envpair',
        title: te('pair.title'),
        desc: te('pair.desc'),
        words: 'pairing pair token another server abbina server',
        block: (
          <PairingBlock
            pairings={envData?.pairings ?? []}
            disabled={srvDisabled}
            onChange={reload}
          />
        ),
      },
    ],
  };
}

function hostsGroup({ t, server, disabled: srvDisabled, set: sv }: ServerRows): GroupDef {
  return {
    id: 'hosts',
    part: 'server',
    icon: 'server',
    hue: 'sw',
    title: t('groups.hosts'),
    admin: true,
    rows: [
      {
        id: 'hostlist',
        title: t('hosts.title'),
        cfgKey: 'hosts = [...]',
        words: 'ssh remote machine',
        block: (
          <HostsBlock
            supported={server.hasKey('hosts')}
            hosts={server.get<HostEntry[]>('hosts', [])}
            disabled={srvDisabled}
            onChange={(next, what) => void sv('hosts', next, what)}
          />
        ),
      },
    ],
  };
}

function updateRows(
  { t, server, disabled: srvDisabled, set: sv }: ServerRows,
  managedBy: string,
): RowDef[] {
  return [
    {
      id: 'uchannel',
      title: t('updates.channel.title'),
      desc: t('updates.channel.desc'),
      words: 'beta rc prerelease stable update',
      cfgKey: `updates.channel = "${server.get('updates.channel', 'stable')}"`,
      control: (
        <Segmented
          aria-label={t('updates.channel.title')}
          value={server.get('updates.channel', 'stable')}
          onChange={(v) =>
            srvDisabled ? undefined : void sv('updates.channel', v, t('updates.channel.title'))
          }
          options={[
            { value: 'stable', label: t('updates.channel.stable') },
            { value: 'prerelease', label: t('updates.channel.prerelease') },
          ]}
        />
      ),
    },
    {
      id: 'uautocheck',
      title: t('updates.autoCheck.title'),
      desc: t('updates.autoCheck.desc'),
      words: 'update notification bell',
      cfgKey: 'updates.auto_check = ' + String(server.get('updates.auto_check', true)),
      control: (
        <Switch
          aria-label={t('updates.autoCheck.title')}
          disabled={srvDisabled}
          checked={server.get('updates.auto_check', true)}
          onChange={(v) => void sv('updates.auto_check', v, t('updates.autoCheck.title'))}
        />
      ),
    },
    {
      id: 'uautoinstall',
      title: t('updates.autoInstall.title'),
      desc:
        managedBy === 'unknown'
          ? t('updates.managedUnknown')
          : managedBy
            ? t('updates.autoInstall.managed', { manager: managedBy, name: Name })
            : t('updates.autoInstall.desc', { name: Name }),
      words: 'update automatic night schedule time',
      cfgKey: `updates.auto_install = ${String(server.get('updates.auto_install', false))} · updates.auto_install_at = "${server.get('updates.auto_install_at', '03:30')}"`,
      control: (
        <>
          <TimeInput
            label={t('updates.autoInstall.at')}
            disabled={srvDisabled || !!managedBy || !server.get('updates.auto_install', false)}
            value={server.get('updates.auto_install_at', '03:30')}
            onCommit={(v) => void sv('updates.auto_install_at', v, t('updates.autoInstall.at'))}
          />
          <Switch
            aria-label={t('updates.autoInstall.title')}
            disabled={srvDisabled || !!managedBy}
            checked={!managedBy && server.get('updates.auto_install', false)}
            onChange={(v) => void sv('updates.auto_install', v, t('updates.autoInstall.title'))}
          />
        </>
      ),
    },
  ];
}

function aboutGroup(
  srv: ServerRows,
  isAdmin: boolean,
  managedBy: string,
  session: Session,
  host: SessionValue['host'],
): GroupDef {
  const { t, server } = srv;
  return {
    id: 'about',
    part: 'about',
    icon: 'info',
    hue: 'sw',
    title: t('groups.about'),
    rows: [
      {
        id: 'brand',
        title: Name,
        words: 'ervisio linuxadmin name logo',
        block: (
          <div className="st-brand">
            <BrandLockup size={30} />
            <small>{t('about.formerly', { name: FormerName })}</small>
          </div>
        ),
      },
      {
        id: 'version',
        title: t('updates.title'),
        words: 'version update upgrade release rollback github',
        block: <UpdatesBlock isAdmin={isAdmin} />,
      },
      ...(isAdmin ? updateRows(srv, managedBy) : []),
      {
        id: 'host',
        title: t('about.host'),
        control: (
          <span className="ui-in ui-in--mono" style={{ minWidth: 180 }}>
            {host?.hostname}
            {host?.ip ? ` (${host.ip})` : ''}
          </span>
        ),
      },
      {
        id: 'user',
        title: t('about.user'),
        control: (
          <span className="ui-in ui-in--mono" style={{ minWidth: 180 }}>
            {session?.user}
          </span>
        ),
      },
      {
        id: 'method',
        title: t('about.method'),
        control: (
          <span className="ui-in ui-in--mono" style={{ minWidth: 180, overflowWrap: 'anywhere' }}>
            {session?.authMethod === 'ssh-key'
              ? t('about.methodKey', { fingerprint: session.keyFingerprint ?? '' })
              : t('about.methodPassword')}
          </span>
        ),
      },
      {
        id: 'conf',
        title: t('about.config'),
        control: (
          <span className="ui-in ui-in--mono" style={{ minWidth: 180 }}>
            {server.path}
          </span>
        ),
      },
    ],
  };
}

const normDur = (v: string) => v;
function withCurrent(opts: { value: string; label: string }[], cur: string) {
  return opts.some((o) => o.value === cur) ? opts : [...opts, { value: cur, label: cur }];
}

/** 24-hour HH:MM picker committed on blur / Enter. */
function TimeInput({
  value,
  onCommit,
  disabled,
  label,
}: {
  value: string;
  onCommit(v: string): void;
  disabled?: boolean;
  label: string;
}) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  const commit = () => {
    if (/^([01]\d|2[0-3]):[0-5]\d$/.test(v) && v !== value) onCommit(v);
    else setV(value);
  };
  return (
    <Input
      compact
      mono
      fieldClassName="st-time"
      type="time"
      step={60}
      aria-label={label}
      disabled={disabled}
      value={v}
      onChange={(e) => setV(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === 'Enter' && commit()}
    />
  );
}

function CommitInput({
  value,
  onCommit,
  disabled,
  label,
  allowEmpty,
}: {
  value: string;
  onCommit(v: string): void;
  disabled?: boolean;
  label: string;
  allowEmpty?: boolean;
}) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  const commit = () => {
    const n = v.trim();
    if ((n || allowEmpty) && n !== value) onCommit(n);
    else setV(value);
  };
  return (
    <Input
      compact
      mono
      aria-label={label}
      disabled={disabled}
      value={v}
      onChange={(e) => setV(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === 'Enter' && commit()}
    />
  );
}
