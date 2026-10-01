import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation } from 'react-router-dom';
import { recentUsersEnabled, setRecentUsersEnabled, useSession, usePrefs } from '../../api';
import { Name } from '../../brand';
import { LANGUAGES, useI18n, useT, type Lang } from '../../i18n';
import { useTheme } from '../../theme';
import { Button, Icon, Input, Segmented, Select, Switch, toast, type HueId, type IconName } from '../../ui';
import { ColourBlock, ThemeGrid } from './Appearance';
import { HostsBlock, type HostEntry } from './Hosts';
import { usePrefSave, useServerConfig } from './save';
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

export default function SettingsPage() {
  const t = useT('settings');
  const { session, host, isUnlocked } = useSession();
  const { prefs } = usePrefs();
  const th = useTheme();
  const { lang, setLang } = useI18n();
  const save = usePrefSave();
  const loc = useLocation();
  const isAdmin = !!session && (session.isAdmin || session.canSudo || !!session.isRoot);
  const server = useServerConfig(isAdmin);
  const [q, setQ] = useState('');
  const [active, setActive] = useState('appearance');
  const [recentOn, setRecentOn] = useState(recentUsersEnabled);
  const setRecent = (on: boolean) => {
    setRecentUsersEnabled(on);
    setRecentOn(on);
    toast.info(t('saved', { name: t('browser.recent.title') }));
  };
  const scrollRef = useRef<HTMLDivElement>(null);

  const region = { timeFormat: '24', weekStart: 'mon', ...(prefs.region ?? {}) } as { timeFormat: '24' | '12'; weekStart: string };
  const term = { theme: 'app', fontSize: 14, copyOnSelect: true, snippets: true, ...(prefs.terminal ?? {}) } as { theme: string; fontSize: number; copyOnSelect: boolean; snippets: boolean };
  const notify = { serviceFail: true, updates: true, sshFail: true, browser: false, ...(prefs.notify ?? {}) } as Record<string, boolean>;
  const setTerm = (patch: Record<string, unknown>) => save('terminal', { ...term, ...patch }, t('groups.terminal'));
  const setRegion = (patch: Record<string, unknown>) => save('region', { ...region, ...patch }, t('groups.language'));
  const setNotify = (key: string, v: boolean, name: string) => {
    if (key === 'browser' && v && typeof Notification !== 'undefined' && Notification.permission === 'default') {
      void Notification.requestPermission().then((p) => {
        if (p !== 'granted') toast.info(t('notify.browserDenied'));
        save('notify', { ...notify, [key]: p === 'granted' }, name);
      });
      return;
    }
    save('notify', { ...notify, [key]: v }, name);
  };

  const themeOptions = (kind: 'dark' | 'light') => th.themes.filter((x) => x.kind === kind).map((x) => ({ value: x.id, label: x.name }));
  const srvDisabled = server.state !== 'ready';
  const sv = server.set;
  const durOptions = DURATIONS.map((d) => ({ value: d, label: t(`dur.${d}`) }));
  const unlockOptions = ['1m', '5m', '15m', '30m'].map((d) => ({ value: d, label: t(`dur.${d}`) }));

  const groups: GroupDef[] = [
    {
      id: 'appearance', part: 'you', icon: 'palette', hue: 'plg', title: t('groups.appearance'),
      rows: [
        { id: 'theme', title: t('appearance.theme.title'), desc: t('appearance.theme.desc'), words: 'dark light oled midnight', block: <ThemeGrid /> },
        { id: 'colours', title: t('appearance.colours.title'), desc: t('appearance.colours.desc'), words: 'distro monochrome colour', block: <ColourBlock /> },
        {
          id: 'follow', title: t('appearance.follow.title'), desc: t('appearance.follow.desc'),
          control: <Switch aria-label={t('appearance.follow.title')} checked={th.follow.on} onChange={(on) => { const s = th.snapshot(); th.setFollow({ ...th.follow, on }); toast.undo(t('saved', { name: t('appearance.follow.title') }), t('undo'), () => th.restore(s)); }} />,
        },
        ...(th.follow.on
          ? [{
              id: 'follow-pair', title: t('appearance.follow.pair'),
              control: (
                <>
                  <Select compact aria-label={t('appearance.dark')} value={th.follow.dark} options={themeOptions('dark')} onChange={(v) => th.setFollow({ ...th.follow, dark: v })} />
                  <Select compact aria-label={t('appearance.light')} value={th.follow.light} options={themeOptions('light')} onChange={(v) => th.setFollow({ ...th.follow, light: v })} />
                </>
              ),
            }]
          : []),
        {
          id: 'density', title: t('appearance.density.title'), desc: t('appearance.density.desc'),
          control: <Segmented aria-label={t('appearance.density.title')} value={th.density} onChange={(d) => { th.setDensity(d); toast.undo(t('saved', { name: t('appearance.density.title') }), t('undo'), () => th.setDensity(th.density)); }} options={[{ value: 'comfortable', label: t('appearance.density.comfortable') }, { value: 'compact', label: t('appearance.density.compact') }]} />,
        },
        {
          id: 'motion', title: t('appearance.motion.title'), desc: t('appearance.motion.desc'),
          control: <Switch aria-label={t('appearance.motion.title')} checked={th.reduceMotion} onChange={(on) => { th.setReduceMotion(on); toast.undo(t('saved', { name: t('appearance.motion.title') }), t('undo'), () => th.setReduceMotion(!on)); }} />,
        },
      ],
    },
    {
      id: 'language', part: 'you', icon: 'globe', hue: 'ov', title: t('groups.language'),
      rows: [
        { id: 'lang', title: t('language.title'), desc: t('language.desc'), control: <Select compact aria-label={t('language.title')} value={lang} options={LANGUAGES.map((l) => ({ value: l.id, label: l.name }))} onChange={(v) => { const before = lang; setLang(v as Lang); toast.undo(t('saved', { name: t('language.title') }), t('undo'), () => setLang(before)); }} /> },
        { id: 'timefmt', title: t('language.time'), control: <Segmented aria-label={t('language.time')} value={region.timeFormat} onChange={(v) => setRegion({ timeFormat: v })} options={[{ value: '24', label: t('language.h24') }, { value: '12', label: t('language.h12') }]} /> },
        { id: 'week', title: t('language.week'), control: <Select compact aria-label={t('language.week')} value={region.weekStart} onChange={(v) => setRegion({ weekStart: v })} options={[{ value: 'mon', label: t('language.mon') }, { value: 'sun', label: t('language.sun') }, { value: 'sat', label: t('language.sat') }]} /> },
      ],
    },
    {
      id: 'terminal', part: 'you', icon: 'terminal', hue: 'term', title: t('groups.terminal'),
      rows: [
        { id: 'tcolour', title: t('terminal.colour'), desc: t('terminal.colourDesc'), control: <Select compact aria-label={t('terminal.colour')} value={term.theme} onChange={(v) => setTerm({ theme: v })} options={[{ value: 'app', label: t('terminal.followApp') }, { value: 'dark', label: t('appearance.dark') }, { value: 'light', label: t('appearance.light') }]} /> },
        { id: 'tfont', title: t('terminal.font'), control: <Segmented aria-label={t('terminal.font')} value={String(term.fontSize)} onChange={(v) => setTerm({ fontSize: Number(v) })} options={['12', '13', '14', '16'].map((n) => ({ value: n, label: n }))} /> },
        { id: 'tcopy', title: t('terminal.copy'), control: <Switch aria-label={t('terminal.copy')} checked={term.copyOnSelect} onChange={(v) => setTerm({ copyOnSelect: v })} /> },
        { id: 'tsnip', title: t('terminal.snippets'), desc: t('terminal.snippetsDesc'), control: <Switch aria-label={t('terminal.snippets')} checked={term.snippets} onChange={(v) => setTerm({ snippets: v })} /> },
      ],
    },
    {
      id: 'notifications', part: 'you', icon: 'bell', hue: 'log', title: t('groups.notifications'),
      rows: [
        { id: 'nsvc', title: t('notify.service'), control: <Switch aria-label={t('notify.service')} checked={notify.serviceFail} onChange={(v) => setNotify('serviceFail', v, t('notify.service'))} /> },
        { id: 'nupd', title: t('notify.updates'), control: <Switch aria-label={t('notify.updates')} checked={notify.updates} onChange={(v) => setNotify('updates', v, t('notify.updates'))} /> },
        { id: 'nssh', title: t('notify.ssh'), desc: t('notify.sshDesc'), control: <Switch aria-label={t('notify.ssh')} checked={notify.sshFail} onChange={(v) => setNotify('sshFail', v, t('notify.ssh'))} /> },
        { id: 'nbrowser', title: t('notify.browser'), desc: t('notify.browserDesc', { name: Name }), control: <Switch aria-label={t('notify.browser')} checked={notify.browser} onChange={(v) => setNotify('browser', v, t('notify.browser'))} /> },
      ],
    },
    {
      id: 'browser', part: 'you', icon: 'shield', hue: 'usr', title: t('groups.browser'),
      rows: [
        { id: 'recentusers', title: t('browser.recent.title'), desc: t('browser.recent.desc'), words: 'sign-in login accounts names privacy shared', control: <Switch aria-label={t('browser.recent.title')} checked={recentOn} onChange={setRecent} /> },
      ],
    },
  ];

  const serverTop = (
    <>
      {server.state === 'failed' && (
        <div className="st-lock" role="alert">
          <Icon name="alert" size={22} />
          <div className="grow"><b>{t('server.failed.title')}</b><small>{t('server.failed.text')}</small></div>
          <Button icon="refresh" onClick={() => void server.reload()}>{t('server.failed.action')}</Button>
        </div>
      )}
      <div className="st-warn"><Icon name="alert" /><div>{t('server.warn')}{isUnlocked ? '' : ' ' + t('server.askPassword')}</div></div>
      {server.warnings.length > 0 && <div className="st-warn"><Icon name="info" /><div>{t('server.unknownKeys', { keys: server.warnings.join(', ') })}</div></div>}
    </>
  );

  if (isAdmin) {
    groups.push(
      {
        id: 'signin', part: 'server', icon: 'key', hue: 'svc', title: t('groups.signin'), admin: true, top: serverTop,
        rows: [
          { id: 'root', title: t('signin.root'), desc: t('signin.rootDesc'), cfgKey: 'allow_root = ' + String(server.get('allow_root', false)), control: <Switch aria-label={t('signin.root')} disabled={srvDisabled} checked={server.get('allow_root', false)} onChange={(v) => void sv('allow_root', v, t('signin.root'))} /> },
          { id: 'showip', title: t('signin.showIp'), desc: t('signin.showIpDesc'), cfgKey: 'login.show_ip = ' + String(server.get('login.show_ip', true)), control: <Switch aria-label={t('signin.showIp')} disabled={srvDisabled} checked={server.get('login.show_ip', true)} onChange={(v) => void sv('login.show_ip', v, t('signin.showIp'))} /> },
          { id: 'timeout', title: t('signin.timeout'), cfgKey: `session.timeout = "${server.get('session.timeout', '12h')}"`, control: <Select compact aria-label={t('signin.timeout')} disabled={srvDisabled} value={normDur(server.get('session.timeout', '12h'))} options={withCurrent(durOptions, server.get('session.timeout', '12h'))} onChange={(v) => void sv('session.timeout', toGoDur(v), t('signin.timeout'))} /> },
          { id: 'unlock', title: t('signin.unlock'), desc: t('signin.unlockDesc'), cfgKey: `session.admin_unlock = "${server.get('session.admin_unlock', '5m')}"`, control: <Select compact aria-label={t('signin.unlock')} disabled={srvDisabled} value={server.get('session.admin_unlock', '5m')} options={withCurrent(unlockOptions, server.get('session.admin_unlock', '5m'))} onChange={(v) => void sv('session.admin_unlock', v, t('signin.unlock'))} /> },
          { id: 'failures', title: t('signin.failures'), desc: t('signin.failuresDesc'), cfgKey: `login.max_failures = ${server.get('login.max_failures', 5)}`, control: <Select compact aria-label={t('signin.failures')} disabled={srvDisabled} value={String(server.get('login.max_failures', 5))} options={withCurrent(['3', '5', '10', '20'].map((n) => ({ value: n, label: t('signin.attempts', { count: Number(n) }) })), String(server.get('login.max_failures', 5)))} onChange={(v) => void sv('login.max_failures', Number(v), t('signin.failures'))} /> },
        ],
      },
      {
        id: 'web', part: 'server', icon: 'net', hue: 'file', title: t('groups.web'), admin: true,
        rows: [
          { id: 'listen', title: t('web.listen'), restart: server.needsRestart('listen'), cfgKey: `listen = "${server.get('listen', '0.0.0.0:9090')}"`, control: <CommitInput disabled={srvDisabled} value={server.get('listen', '0.0.0.0:9090')} label={t('web.listen')} onCommit={(v) => void sv('listen', v, t('web.listen'))} /> },
          { id: 'tls', title: t('web.tls'), desc: t('web.tlsDesc'), restart: server.needsRestart('tls.mode'), cfgKey: `tls.mode = "${server.get('tls.mode', 'self-signed')}"`, control: <Segmented aria-label={t('web.tls')} value={server.get('tls.mode', 'self-signed')} onChange={(v) => srvDisabled ? undefined : void sv('tls.mode', v, t('web.tls'))} options={[{ value: 'self-signed', label: t('web.selfSigned') }, { value: 'letsencrypt', label: t('web.letsencrypt') }, { value: 'custom', label: t('web.custom') }]} /> },
          { id: 'redirect', title: t('web.redirect'), restart: server.needsRestart('tls.redirect'), cfgKey: 'tls.redirect = ' + String(server.get('tls.redirect', true)), control: <Switch aria-label={t('web.redirect')} disabled={srvDisabled} checked={server.get('tls.redirect', true)} onChange={(v) => void sv('tls.redirect', v, t('web.redirect'))} /> },
          ...(server.get<string>('tls.mode', 'self-signed') === 'custom'
            ? [
                { id: 'cert', title: t('web.cert'), desc: t('web.certDesc'), restart: true, cfgKey: `tls.cert = "${server.get('tls.cert', '')}"`, control: <CommitInput disabled={srvDisabled} allowEmpty value={server.get('tls.cert', '')} label={t('web.cert')} onCommit={(v) => void sv('tls.cert', v, t('web.cert'))} /> },
                { id: 'key', title: t('web.key'), restart: true, cfgKey: `tls.key = "${server.get('tls.key', '')}"`, control: <CommitInput disabled={srvDisabled} allowEmpty value={server.get('tls.key', '')} label={t('web.key')} onCommit={(v) => void sv('tls.key', v, t('web.key'))} /> },
              ]
            : []),
        ],
      },
      {
        id: 'plugpol', part: 'server', icon: 'plugins', hue: 'plg', title: t('groups.plugpol'), admin: true,
        rows: [
          { id: 'unsigned', title: t('plugpol.unsigned'), desc: t('plugpol.unsignedDesc', { name: Name }), cfgKey: 'plugins.allow_unsigned = ' + String(server.get('plugins.allow_unsigned', false)), control: <Switch aria-label={t('plugpol.unsigned')} disabled={srvDisabled} checked={server.get('plugins.allow_unsigned', false)} onChange={(v) => void sv('plugins.allow_unsigned', v, t('plugpol.unsigned'))} /> },
          { id: 'dev', title: t('plugpol.dev'), desc: t('plugpol.devDesc'), cfgKey: 'plugins.dev = ' + String(server.get('plugins.dev', false)), control: <Switch aria-label={t('plugpol.dev')} disabled={srvDisabled} checked={server.get('plugins.dev', false)} onChange={(v) => void sv('plugins.dev', v, t('plugpol.dev'))} /> },
        ],
      },
      {
        id: 'hosts', part: 'server', icon: 'server', hue: 'sw', title: t('groups.hosts'), admin: true,
        rows: [
          { id: 'hostlist', title: t('hosts.title'), cfgKey: 'hosts = [...]', words: 'ssh remote machine', block: <HostsBlock supported={server.hasKey('hosts')} hosts={server.get<HostEntry[]>('hosts', [])} disabled={srvDisabled} onChange={(next, what) => void sv('hosts', next, what)} /> },
        ],
      },
    );
  }
  groups.push({
    id: 'about', part: 'about', icon: 'info', hue: 'sw', title: t('groups.about'),
    rows: [
      { id: 'version', title: `${Name} 0.1.0`, desc: t('about.versionDesc') },
      { id: 'host', title: t('about.host'), control: <span className="ui-in ui-in--mono" style={{ minWidth: 180 }}>{host?.hostname}{host?.ip ? ` (${host.ip})` : ''}</span> },
      { id: 'user', title: t('about.user'), control: <span className="ui-in ui-in--mono" style={{ minWidth: 180 }}>{session?.user}</span> },
      { id: 'conf', title: t('about.config'), control: <span className="ui-in ui-in--mono" style={{ minWidth: 180 }}>{server.path}</span> },
    ],
  });

  /* ---------- search ---------- */
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const gHit = (str: string) => words.every((w) => str.toLowerCase().includes(w));
  const visible = groups
    .map((g) => ({
      g,
      rows: words.length ? g.rows.filter((r) => gHit(`${g.title} ${r.title} ${r.desc ?? ''} ${r.cfgKey ?? ''} ${r.words ?? ''}`)) : g.rows,
    }))
    .filter((x) => x.rows.length > 0);

  /* ---------- scroll spy and deep links ---------- */
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
    if (el && scrollRef.current) scrollRef.current.scrollTo({ top: el.offsetTop - scrollRef.current.offsetTop - 10 });
    setActive(id);
  };
  useEffect(() => {
    const id = loc.hash.replace('#', '');
    if (id) window.setTimeout(() => go(id), 50);
  }, [loc.hash, server.state]);

  const navGroups = (part: GroupDef['part']) => groups.filter((g) => g.part === part);
  const partTitle = (p: GroupDef['part']) => (p === 'you' ? t('parts.you') : p === 'server' ? t('parts.server') : Name);

  return (
    <div className="st hue-set">
      <nav className="st-nav" aria-label={t('title')}>
        <h2>{t('title')}</h2>
        {(['you', 'server', 'about'] as const).map((p) =>
          navGroups(p).length ? (
            <div key={p} style={{ display: 'contents' }}>
              <div className="st-gl">{partTitle(p)}</div>
              {navGroups(p).map((g) => (
                <button key={g.id} type="button" className="st-ni" aria-current={active === g.id} onClick={() => go(g.id)}>
                  <span className={`st-tile hue-${g.hue}`}><Icon name={g.icon} /></span>
                  {g.title}
                  {g.admin && <Icon name="lock" />}
                </button>
              ))}
            </div>
          ) : null,
        )}
      </nav>
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
              {!words.length && (i === 0 || visible[i - 1].g.part !== g.part) && <div className="st-part">{g.part === 'server' ? t('parts.serverAdmins') : partTitle(g.part)}</div>}
              <h3 className="st-gt" id={`st-${g.id}`}>
                <span className={`st-tile hue-${g.hue}`}><Icon name={g.icon} /></span>
                {g.title}
                {g.admin && <span className="st-lockb"><Icon name="lock" />{t('adminOnly')}</span>}
              </h3>
              <div className={`st-grp${g.admin && server.state !== 'ready' ? ' st-grp--locked' : ''}`}>
                {g.top}
                {rows.map((r) =>
                  r.block ? (
                    <div className="st-row st-row--block" key={r.id}>
                      <div className="st-tx" style={{ marginBottom: 12 }}>
                        <b>{r.title}</b>
                        {r.desc && <small>{r.desc}</small>}
                        {r.cfgKey && <span className="st-key">{r.cfgKey}{r.restart ? ` · ${t('server.restart')}` : ''}</span>}
                      </div>
                      {r.block}
                    </div>
                  ) : (
                    <div className="st-row" key={r.id}>
                      <div className="st-tx">
                        <b>{r.title}</b>
                        {r.desc && <small>{r.desc}</small>}
                        {r.cfgKey && <span className="st-key">{r.cfgKey}{r.restart ? ` · ${t('server.restart')}` : ''}</span>}
                      </div>
                      {r.control && <div className="st-ctl">{r.control}</div>}
                    </div>
                  ),
                )}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}

const normDur = (v: string) => v;
function withCurrent(opts: { value: string; label: string }[], cur: string) {
  return opts.some((o) => o.value === cur) ? opts : [...opts, { value: cur, label: cur }];
}

function CommitInput({ value, onCommit, disabled, label, allowEmpty }: { value: string; onCommit(v: string): void; disabled?: boolean; label: string; allowEmpty?: boolean }) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  const commit = () => {
    const n = v.trim();
    if ((n || allowEmpty) && n !== value) onCommit(n);
    else setV(value);
  };
  return <Input compact mono aria-label={label} disabled={disabled} value={v} onChange={(e) => setV(e.target.value)} onBlur={commit} onKeyDown={(e) => e.key === 'Enter' && commit()} />;
}
