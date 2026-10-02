import { useEffect, useState } from 'react';
import { ApiError, useSession } from '../../api';
import { useT } from '../../i18n';
import { formatDateTime } from '../../lib/format';
import { Button, ConfirmDialog, Dialog, IconButton, Input, toast } from '../../ui';
import { pairTokenCreate, pairingRevoke, type PairToken, type Pairing } from './envs';
import './envs.css';

const errText = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e));

const when = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000 ? '' : formatDateTime(d);
};

/** Pairing, server side: lets another Ervisio server use the Docker of this one (its Environments > Ervisio server). */
export function PairingBlock({ pairings, onChange, disabled }: { pairings: Pairing[]; onChange(): void; disabled?: boolean }) {
  const t = useT('envs');
  const { session } = useSession();
  const [open, setOpen] = useState(false);
  const [revoking, setRevoking] = useState<Pairing | null>(null);
  return (
    <>
      {pairings.length === 0 && <div className="env-empty"><small>{t('pair.none')}</small></div>}
      {pairings.map((p) => (
        <div className="st-host" key={p.id}>
          <span className="dot" style={{ background: p.lastUsed && when(p.lastUsed) ? 'var(--ok)' : 'var(--ink3)' }} />
          <div className="grow">
            <b>{p.name}</b>
            <small>
              {t('pair.runsAs', { user: p.user })} · {t('pair.created', { date: when(p.created) })}
              {when(p.lastUsed) ? ` · ${t('pair.lastUsed', { date: when(p.lastUsed) })}${p.lastVia ? ` · ${t('pair.lastVia', { via: p.lastVia })}` : ''}` : ` · ${t('pair.neverUsed')}`}
            </small>
          </div>
          <IconButton icon="trash" label={t('pair.revoke', { name: p.name })} disabled={disabled} onClick={() => setRevoking(p)} />
        </div>
      ))}
      <div style={{ padding: '10px 0' }}>
        <Button icon="link" disabled={disabled} onClick={() => setOpen(true)}>{t('pair.create')}</Button>
      </div>
      {open && <TokenDialog defaultUser={session?.user ?? ''} onClose={() => { setOpen(false); onChange(); }} />}
      <ConfirmDialog
        open={!!revoking}
        onClose={() => setRevoking(null)}
        onConfirm={async () => {
          if (!revoking) return;
          await pairingRevoke(revoking.id);
          toast.ok(t('pair.revoked', { name: revoking.name }));
          onChange();
        }}
        title={t('pair.revokeTitle', { name: revoking?.name ?? '' })}
        description={t('pair.revokeText', { name: revoking?.name ?? '' })}
        confirmLabel={t('pair.revokeConfirm')}
        icon="trash"
      />
    </>
  );
}

function TokenDialog({ defaultUser, onClose }: { defaultUser: string; onClose(): void }) {
  const t = useT('envs');
  const [user, setUser] = useState(defaultUser);
  const [tok, setTok] = useState<PairToken | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [left, setLeft] = useState(0);
  useEffect(() => {
    if (!tok) return;
    const end = Date.parse(tok.expires);
    const tick = () => setLeft(Math.max(0, Math.round((end - Date.now()) / 1000)));
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [tok]);
  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (tok) return;
    setBusy(true);
    setErr('');
    try {
      setTok(await pairTokenCreate(user.trim() || undefined));
    } catch (e2) {
      setErr(errText(e2));
    } finally {
      setBusy(false);
    }
  };
  const copy = () => {
    if (!tok) return;
    void navigator.clipboard?.writeText(tok.token).then(() => toast.ok(t('pair.copied')), () => toast.err(t('pair.copyFailed')));
  };
  const mm = String(Math.floor(left / 60)).padStart(2, '0');
  const ss = String(left % 60).padStart(2, '0');
  return (
    <Dialog
      open
      onClose={onClose}
      size="lg"
      icon="link"
      title={t('pair.dialogTitle')}
      description={tok ? t('pair.tokenText', { minutes: tok.ttlMinutes }) : t('pair.dialogText')}
      onSubmit={(e) => void create(e)}
      footer={
        <>
          {err && <span className="env-foot-err" role="alert">{err}</span>}
          {tok ? (
            <Button type="button" variant="primary" onClick={onClose}>{t('pair.done')}</Button>
          ) : (
            <>
              <Button variant="ghost" onClick={onClose}>{t('cancel')}</Button>
              <Button type="submit" variant="primary" disabled={busy}>{t('pair.createToken')}</Button>
            </>
          )}
        </>
      }
    >
      <div className="env-form">
        {!tok ? (
          <>
            <Input data-autofocus label={t('pair.user')} icon="user" value={user} onChange={(e) => setUser(e.target.value)} hint={t('pair.userHint')} />
            <div className="env-warn" role="note">{t('pair.securityNote', { user: user || defaultUser })}</div>
          </>
        ) : (
          <>
            <div className="env-token" aria-live="polite">
              <code>{tok.token}</code>
              <Button icon="copy" onClick={copy}>{t('pair.copy')}</Button>
            </div>
            <small className="env-hint">{left > 0 ? t('pair.expiresIn', { time: `${mm}:${ss}` }) : t('pair.expired')}</small>
            <div className="env-fp">
              <small>{t('pair.fingerprint', { server: tok.server })}</small>
              <code>{tok.fingerprint || t('pair.noTls')}</code>
              <small className="env-hint">{tok.fingerprint ? t('pair.fingerprintHint') : t('pair.noTlsHint')}</small>
            </div>
            <small className="env-hint">{t('pair.runsAsNote', { user: tok.user })}</small>
          </>
        )}
      </div>
    </Dialog>
  );
}
