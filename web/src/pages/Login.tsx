import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { ApiError, readRecentUsers, rememberRecentUser, useSession, type RecentUser } from '../api';
import { SshKeyError, type SshKeyErrorCode } from '../auth/sshkey';
import { KeyStep, type LoadedKey } from './KeyStep';
import { useT, type TFn } from '../i18n';
import { useTheme } from '../theme';
import { Button, Checkbox, Icon, IconButton, Input, BrandLockup } from '../ui';
import { DistroLogo } from '../shell/DistroLogo';
import './login.css';

// Recent accounts are stored only when the user opted in (see api/recentUsers).
const readRecent = readRecentUsers;
const remember = rememberRecentUser;
const TILE_HUES = ['usr', 'file', 'log'];

type Problem = null | {
  kind: 'key' | 'wrong' | 'root' | 'rate' | 'busy' | 'forbidden' | 'network' | 'other';
  text?: string;
  minutes?: number;
  code?: SshKeyErrorCode;
  seconds?: number;
};

function classify(e: unknown): Problem {
  if (!(e instanceof ApiError)) return { kind: 'other', text: String(e) };
  const d = (e.data ?? {}) as { reason?: string; retryAfter?: number };
  if (d.reason === 'root_disabled' || (e.code === 'forbidden' && /root/i.test(e.message)))
    return { kind: 'root' };
  if (d.reason === 'busy') return { kind: 'busy' };
  if (e.status === 429 || d.reason === 'rate_limited' || /too many|rate/i.test(e.message))
    return {
      kind: 'rate',
      minutes: d.retryAfter ? Math.max(1, Math.ceil(d.retryAfter / 60)) : undefined,
    };
  if (e.code === 'forbidden') return { kind: 'forbidden', text: e.message };
  if (e.code === 'unauthenticated' || e.code === 'invalid') return { kind: 'wrong' };
  if (e.code === 'network') return { kind: 'network' };
  return { kind: 'other', text: e.message };
}

function classifyKey(e: unknown): NonNullable<Problem> {
  if (e instanceof SshKeyError) return { kind: 'key', code: e.code, seconds: e.retryAfter };
  return { kind: 'key', code: 'server_error' };
}

/** Message hue and icon per key error code (red = the key or passphrase is wrong, amber = wait or policy). */
const KEY_MSG: Record<SshKeyErrorCode, { icon: string; hue: string }> = {
  not_a_key: { icon: 'alert', hue: 'hue-svc' },
  public_key_given: { icon: 'alert', hue: 'hue-svc' },
  unsupported_format: { icon: 'alert', hue: 'hue-svc' },
  passphrase_required: { icon: 'lock', hue: 'hue-log' },
  bad_passphrase: { icon: 'lock', hue: 'hue-svc' },
  unsupported_key_type: { icon: 'alert', hue: 'hue-svc' },
  unsupported_cipher: { icon: 'alert', hue: 'hue-svc' },
  corrupt_key: { icon: 'alert', hue: 'hue-svc' },
  crypto_unavailable: { icon: 'shield', hue: 'hue-log' },
  key_refused: { icon: 'alert', hue: 'hue-svc' },
  challenge_invalid: { icon: 'clock', hue: 'hue-log' },
  ssh_keys_disabled: { icon: 'lock', hue: 'hue-log' },
  root_disabled: { icon: 'shield', hue: 'hue-log' },
  dev_mode_user: { icon: 'shield', hue: 'hue-log' },
  host_not_allowed: { icon: 'shield', hue: 'hue-log' },
  rate_limited: { icon: 'clock', hue: 'hue-log' },
  busy: { icon: 'clock', hue: 'hue-log' },
  network: { icon: 'alert', hue: 'hue-svc' },
  server_error: { icon: 'alert', hue: 'hue-svc' },
};

type Message = { icon: string; hue: string; body: ReactNode };

function keyWaitVars(t: TFn, seconds?: number) {
  if (seconds && seconds > 90)
    return { wait: t('key.waitMinutes', { minutes: Math.ceil(seconds / 60) }) };
  if (seconds) return { wait: t('key.waitSeconds', { seconds: Math.ceil(seconds) }) };
  return { wait: t('key.waitLater') };
}

function keyMessage(problem: NonNullable<Problem>, t: TFn): Message {
  const code = problem.code ?? 'server_error';
  const k = KEY_MSG[code];
  return { icon: k.icon, hue: k.hue, body: t(`err.key.${code}`, keyWaitVars(t, problem.seconds)) };
}

function rateBody(problem: NonNullable<Problem>, t: TFn) {
  return problem.minutes ? t('err.rateMinutes', { minutes: problem.minutes }) : t('err.rate');
}

function rootBody(t: TFn) {
  return (
    <>
      {t('err.root.before')} <code>allow_root = true</code> {t('err.root.after')}
    </>
  );
}

function problemMessage(problem: Problem, t: TFn): Message | null {
  if (!problem) return null;
  switch (problem.kind) {
    case 'key':
      return keyMessage(problem, t);
    case 'wrong':
      return { icon: 'alert', hue: 'hue-svc', body: t('err.wrong') };
    case 'root':
      return { icon: 'shield', hue: 'hue-log', body: rootBody(t) };
    case 'busy':
      return { icon: 'clock', hue: 'hue-log', body: t('err.busy') };
    case 'rate':
      return { icon: 'clock', hue: 'hue-log', body: rateBody(problem, t) };
    case 'forbidden':
      return { icon: 'lock', hue: 'hue-svc', body: problem.text || t('err.forbidden') };
    case 'network':
      return { icon: 'alert', hue: 'hue-svc', body: t('err.network') };
    default:
      return { icon: 'alert', hue: 'hue-svc', body: problem.text || t('err.other') };
  }
}

type Host = ReturnType<typeof useSession>['host'];

function HostArt({ host }: { host: Host }) {
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

function RecentAccounts({ t, recent, user, other, onPick, onOther }: RecentAccountsProps) {
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

type Method = 'password' | 'key';

function MethodSwitch({
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

export default function LoginPage() {
  const t = useT('auth');
  const { status, host, signIn, signInWithKey } = useSession();
  const { isDark, toggleDark } = useTheme();
  const loc = useLocation();
  const recent = useMemo(readRecent, []);
  const [user, setUser] = useState(recent[0]?.user ?? '');
  const [other, setOther] = useState(recent.length === 0);
  const [pw, setPw] = useState('');
  const [show, setShow] = useState(false);
  const [stay, setStay] = useState(true);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<Problem>(null);
  const keysOn = host?.sshKeys === true;
  const [method, setMethod] = useState<Method>(recent[0]?.method === 'key' ? 'key' : 'password');
  const keyMode = keysOn && method === 'key';
  const [loadedKey, setLoadedKey] = useState<LoadedKey | null>(null);
  const [passphrase, setPassphrase] = useState('');
  const pwRef = useRef<HTMLInputElement>(null);
  const userRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    document.title = `${t('title')} · ${host?.hostname ?? ''}`;
  }, [t, host]);
  useEffect(() => {
    (other ? userRef : pwRef).current?.focus();
  }, [other]);

  useEffect(
    () => () => {
      // Nothing of the key is kept: the state goes with the component, and this drops the references early.
      setLoadedKey(null);
      setPassphrase('');
    },
    [],
  );

  if (status === 'authed')
    return <Navigate to={(loc.state as { from?: string } | null)?.from ?? '/'} replace />;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const u = user.trim();
    if (!u || !pw) {
      setProblem({ kind: 'wrong' });
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const r = await signIn(u, pw, stay);
      remember({ user: u, isAdmin: r.isAdmin });
    } catch (x) {
      setProblem(classify(x));
      setPw('');
      pwRef.current?.focus();
    } finally {
      setBusy(false);
    }
  };

  const keyReady = !!loadedKey && (!loadedKey.needsPass || passphrase.length > 0);
  const submitKey = async () => {
    const u = user.trim();
    if (!loadedKey || busy) return;
    if (!u) {
      setProblem({ kind: 'key', code: 'key_refused' });
      userRef.current?.focus();
      return;
    }
    if (!keyReady) return;
    setBusy(true);
    setProblem(null);
    try {
      const r = await signInWithKey({
        user: u,
        keyText: loadedKey.text,
        passphrase: loadedKey.needsPass ? passphrase : undefined,
        stay,
      });
      remember({ user: u, isAdmin: r.isAdmin, method: 'key' });
      setLoadedKey(null);
      setPassphrase('');
    } catch (x) {
      const p = classifyKey(x);
      setProblem(p);
      if (p.code === 'bad_passphrase') setPassphrase('');
    } finally {
      setBusy(false);
    }
  };
  const switchMethod = (m: Method) => {
    setMethod(m);
    setProblem(null);
    setLoadedKey(null);
    setPassphrase('');
    setPw('');
  };
  const pickRecent = (r: RecentUser) => {
    setOther(false);
    setUser(r.user);
    setProblem(null);
    if (r.method === 'key' && keysOn) switchMethod('key');
    else if (method === 'key' && r.method !== 'key') switchMethod('password');
    else pwRef.current?.focus();
  };
  const pickOther = () => {
    setOther(true);
    setUser('');
    setProblem(null);
  };

  const msg = problemMessage(problem, t);

  return (
    <div className="login">
      <HostArt host={host} />
      <section className="lb-form">
        <div className="lb-in">
          <h1>{t('title')}</h1>
          <p>{recent.length ? t('pick') : t('enter')}</p>
          {recent.length > 0 && (
            <RecentAccounts
              t={t}
              recent={recent}
              user={user}
              other={other}
              onPick={pickRecent}
              onOther={pickOther}
            />
          )}
          <form
            className="lf"
            onSubmit={
              keyMode
                ? (e) => {
                    e.preventDefault();
                    void submitKey();
                  }
                : submit
            }
            noValidate
          >
            {other && (
              <Input
                ref={userRef}
                label={t('username')}
                icon="user"
                id="u"
                name="username"
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                placeholder={t('usernamePlaceholder')}
                value={user}
                onChange={(e) => setUser(e.target.value)}
              />
            )}
            {keyMode ? (
              <KeyStep
                loaded={loadedKey}
                onLoaded={(k) => {
                  setLoadedKey(k);
                  setProblem(null);
                }}
                onClear={() => setLoadedKey(null)}
                onError={(code) => setProblem(code ? { kind: 'key', code } : null)}
                passphrase={passphrase}
                onPassphrase={setPassphrase}
                passError={
                  problem?.code === 'bad_passphrase' || problem?.code === 'passphrase_required'
                }
                disabled={busy}
              />
            ) : (
              <Input
                ref={pwRef}
                label={t('password')}
                icon="shield"
                id="p"
                name="password"
                type={show ? 'text' : 'password'}
                autoComplete="current-password"
                placeholder={t('passwordPlaceholder')}
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                error={problem?.kind === 'wrong' ? true : undefined}
                end={
                  <IconButton
                    icon={show ? 'eyeoff' : 'eye'}
                    label={show ? t('hidePassword') : t('showPassword')}
                    onClick={() => setShow((s) => !s)}
                  />
                }
              />
            )}
            {msg && (
              <div className={`lmsg ${msg.hue}`} role="alert">
                <Icon name={msg.icon} />
                <div>{msg.body}</div>
              </div>
            )}
            <Checkbox checked={stay} onChange={setStay} label={t('stay')} />
            <Button
              type="submit"
              variant="primary"
              size="lg"
              block
              loading={busy}
              disabled={keyMode && !keyReady}
            >
              {busy && keyMode ? t('key.signingIn') : t('submit')}
            </Button>
            {keysOn && <MethodSwitch t={t} keyMode={keyMode} busy={busy} onSwitch={switchMethod} />}
            {keyMode && (
              <p className="sk-note">
                <Icon name="shield" />
                <span>{t('key.note')}</span>
              </p>
            )}
          </form>
          {recent.length > 0 && <p className="lb-note">{t('footnote')}</p>}
          <div className="lb-foot">
            <BrandLockup size={20} />
            <button type="button" onClick={toggleDark}>
              <Icon name={isDark ? 'moon' : 'sun'} size={15} />
              {t('theme')}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
