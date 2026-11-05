import { useMemo, useState, type FormEvent } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { readRecentUsers, useSession, type RecentUser } from '../api';
import { KeyStep } from './KeyStep';
import { useT } from '../i18n';
import { useTheme } from '../theme';
import { Button, Checkbox } from '../ui';
import { problemMessage, type Problem } from './loginProblem';
import {
  HostArt,
  KeyNote,
  LoginAlert,
  LoginFooter,
  MethodSwitch,
  PasswordField,
  RecentAccounts,
  UsernameField,
  type Method,
} from './LoginParts';
import { useKeySignIn, useLoginFocus, usePasswordSignIn } from './useLoginFlow';
import './login.css';

export default function LoginPage() {
  const t = useT('auth');
  const { status, host } = useSession();
  const { isDark, toggleDark } = useTheme();
  const loc = useLocation();
  // Recent accounts are stored only when the user opted in (see api/recentUsers).
  const recent = useMemo(readRecentUsers, []);
  const [user, setUser] = useState(recent[0]?.user ?? '');
  const [other, setOther] = useState(recent.length === 0);
  const [show, setShow] = useState(false);
  const [stay, setStay] = useState(true);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<Problem>(null);
  const keysOn = host?.sshKeys === true;
  const [method, setMethod] = useState<Method>(recent[0]?.method === 'key' ? 'key' : 'password');
  const keyMode = keysOn && method === 'key';
  const { pwRef, userRef } = useLoginFocus(`${t('title')} · ${host?.hostname ?? ''}`, other);
  const signInContext = { user, stay, setBusy, setProblem };
  const { pw, setPw, submit } = usePasswordSignIn({ ...signInContext, pwRef });
  const { loadedKey, setLoadedKey, passphrase, setPassphrase, keyReady, submitKey } = useKeySignIn({
    ...signInContext,
    busy,
    userRef,
  });

  if (status === 'authed')
    return <Navigate to={(loc.state as { from?: string } | null)?.from ?? '/'} replace />;

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
  const submitForm = keyMode
    ? (e: FormEvent) => {
        e.preventDefault();
        void submitKey();
      }
    : submit;
  const hasRecent = recent.length > 0;

  return (
    <div className="login">
      <HostArt host={host} />
      <section className="lb-form">
        <div className="lb-in">
          <h1>{t('title')}</h1>
          <p>{hasRecent ? t('pick') : t('enter')}</p>
          {hasRecent && (
            <RecentAccounts
              t={t}
              recent={recent}
              user={user}
              other={other}
              onPick={pickRecent}
              onOther={pickOther}
            />
          )}
          <form className="lf" onSubmit={submitForm} noValidate>
            {other && <UsernameField ref={userRef} t={t} value={user} onChange={setUser} />}
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
              <PasswordField
                ref={pwRef}
                t={t}
                value={pw}
                onChange={setPw}
                isWrong={problem?.kind === 'wrong'}
                isShown={show}
                onToggleShown={() => setShow((s) => !s)}
              />
            )}
            <LoginAlert message={problemMessage(problem, t)} />
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
            {keyMode && <KeyNote t={t} />}
          </form>
          {hasRecent && <p className="lb-note">{t('footnote')}</p>}
          <LoginFooter t={t} isDark={isDark} onToggleDark={toggleDark} />
        </div>
      </section>
    </div>
  );
}
