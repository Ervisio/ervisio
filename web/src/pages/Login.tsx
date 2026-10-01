import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { ApiError, readRecentUsers, rememberRecentUser, useSession } from '../api';
import { useT } from '../i18n';
import { useTheme } from '../theme';
import { Button, Checkbox, Icon, IconButton, Input } from '../ui';
import { DistroLogo } from '../shell/DistroLogo';
import './login.css';

// Recent accounts are stored only when the user opted in (see api/recentUsers).
const readRecent = readRecentUsers;
const remember = rememberRecentUser;
const TILE_HUES = ['usr', 'file', 'log'];

type Problem = null | { kind: 'wrong' | 'root' | 'rate' | 'busy' | 'forbidden' | 'network' | 'other'; text?: string; minutes?: number };

function classify(e: unknown): Problem {
  if (!(e instanceof ApiError)) return { kind: 'other', text: String(e) };
  const d = (e.data ?? {}) as { reason?: string; retryAfter?: number };
  if (d.reason === 'root_disabled' || (e.code === 'forbidden' && /root/i.test(e.message))) return { kind: 'root' };
  if (d.reason === 'busy') return { kind: 'busy' };
  if (e.status === 429 || d.reason === 'rate_limited' || /too many|rate/i.test(e.message)) return { kind: 'rate', minutes: d.retryAfter ? Math.max(1, Math.ceil(d.retryAfter / 60)) : undefined };
  if (e.code === 'forbidden') return { kind: 'forbidden', text: e.message };
  if (e.code === 'unauthenticated' || e.code === 'invalid') return { kind: 'wrong' };
  if (e.code === 'network') return { kind: 'network' };
  return { kind: 'other', text: e.message };
}

export default function LoginPage() {
  const t = useT('auth');
  const { status, host, signIn } = useSession();
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
  const pwRef = useRef<HTMLInputElement>(null);
  const userRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    document.title = `${t('title')} · ${host?.hostname ?? ''}`;
  }, [t, host]);
  useEffect(() => {
    (other ? userRef : pwRef).current?.focus();
  }, [other]);

  if (status === 'authed') return <Navigate to={(loc.state as { from?: string } | null)?.from ?? '/'} replace />;

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

  const msg = (() => {
    if (!problem) return null;
    switch (problem.kind) {
      case 'wrong': return { icon: 'alert', hue: 'hue-svc', body: t('err.wrong') };
      case 'root': return { icon: 'shield', hue: 'hue-log', body: <>{t('err.root.before')} <code>allow_root = true</code> {t('err.root.after')}</> };
      case 'busy': return { icon: 'clock', hue: 'hue-log', body: t('err.busy') };
      case 'rate': return { icon: 'clock', hue: 'hue-log', body: problem.minutes ? t('err.rateMinutes', { minutes: problem.minutes }) : t('err.rate') };
      case 'forbidden': return { icon: 'lock', hue: 'hue-svc', body: problem.text || t('err.forbidden') };
      case 'network': return { icon: 'alert', hue: 'hue-svc', body: t('err.network') };
      default: return { icon: 'alert', hue: 'hue-svc', body: problem.text || t('err.other') };
    }
  })();

  return (
    <div className="login">
      <section className="lb-art" aria-label={host?.hostname}>
        <span className="glow" />
        <DistroLogo className="big" id={host?.distro.id} logo={host?.distro.logo} url={host?.distro.logoUrl} />
        <div className="nm">
          <h2>{host?.hostname ?? '...'}</h2>
          {host?.ip && <p className="ip">{host.ip}</p>}
          {host?.distro.name && <p className="os">{host.distro.name}</p>}
        </div>
      </section>
      <section className="lb-form">
        <div className="lb-in">
          <h1>{t('title')}</h1>
          <p>{recent.length ? t('pick') : t('enter')}</p>
          {recent.length > 0 && (
            <div className="ugrid" role="group" aria-label={t('recent')}>
              {recent.map((r, i) => (
                <button
                  key={r.user}
                  type="button"
                  className={`ut hue-${TILE_HUES[i % TILE_HUES.length]}`}
                  aria-pressed={!other && user === r.user}
                  onClick={() => {
                    setOther(false);
                    setUser(r.user);
                    setProblem(null);
                    pwRef.current?.focus();
                  }}
                >
                  <span className="av">{r.user.slice(0, 1)}</span>
                  <b>{r.user}</b>
                  <small>{r.isAdmin ? t('role.admin') : t('role.user')}</small>
                </button>
              ))}
              <button
                type="button"
                className="ut other"
                aria-pressed={other}
                onClick={() => {
                  setOther(true);
                  setUser('');
                  setProblem(null);
                }}
              >
                <span className="av"><Icon name="plus" /></span>
                <b>{t('other')}</b>
                <small>{t('otherHint')}</small>
              </button>
            </div>
          )}
          <form className="lf" onSubmit={submit} noValidate>
            {other && (
              <Input ref={userRef} label={t('username')} icon="user" id="u" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} placeholder={t('usernamePlaceholder')} value={user} onChange={(e) => setUser(e.target.value)} />
            )}
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
              end={<IconButton icon={show ? 'eyeoff' : 'eye'} label={show ? t('hidePassword') : t('showPassword')} onClick={() => setShow((s) => !s)} />}
            />
            {msg && (
              <div className={`lmsg ${msg.hue}`} role="alert">
                <Icon name={msg.icon} />
                <div>{msg.body}</div>
              </div>
            )}
            <Checkbox checked={stay} onChange={setStay} label={t('stay')} />
            <Button type="submit" variant="primary" size="lg" block loading={busy}>{t('submit')}</Button>
          </form>
          <div className="lb-foot">
            <span>{recent.length ? t('footnote') : ''}</span>
            <button type="button" onClick={toggleDark}><Icon name={isDark ? 'moon' : 'sun'} size={15} />{t('theme')}</button>
          </div>
        </div>
      </section>
    </div>
  );
}
