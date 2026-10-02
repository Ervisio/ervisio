import { useEffect, useState } from 'react';
import { ApiError, call, useSession } from '../api';
import { useT } from '../i18n';
import { Badge, Button, Dialog } from '../ui';
import './networkApproval.css';

/**
 * A plugin with capabilities.network.userHosts may ask for a host that is not in its manifest
 * (sdk.network.request). The app checks the approvals first; a new host opens this dialog, in the app and not in the
 * plugin's frame, so the plugin cannot draw or answer it. An administrator approves one exact host:port (https, or
 * plain http only when the plugin asks for http and the administrator accepts the risk). Approvals live in
 * Settings > Plugin policy. The plugin's frames reload after an approval: the host becomes part of their policy.
 */

interface Pending {
  plugin: { id: string; name: string };
  host: string;
  scheme: 'https' | 'http';
  settle(r: { approved: boolean }): void;
}
let queue: Pending[] = [];
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());

interface NetResult {
  status: 'approved' | 'pending';
  host: string;
  scheme: 'https' | 'http';
}

/** Resolves with the approved host, or rejects with code "forbidden" when it is refused. */
export async function askNetwork(plugin: { id: string; name: string }, host: string, scheme: 'https' | 'http'): Promise<{ host: string; approved: true; reloading: boolean }> {
  const r = await call<NetResult>('plugins.network.request', { plugin: plugin.id, host, scheme });
  if (r.status === 'approved') return { host: r.host, approved: true, reloading: false };
  const ok = await new Promise<boolean>((resolve) => {
    queue.push({ plugin, host: r.host, scheme: r.scheme, settle: (x) => resolve(x.approved) });
    emit();
  });
  if (!ok) throw new ApiError('forbidden', `${plugin.name} may not connect to ${r.host}: an administrator did not approve it.`);
  return { host: r.host, approved: true, reloading: true };
}

export function NetworkApprovalHost({ reload }: { reload(): void }) {
  const t = useT('envs');
  const { session } = useSession();
  const [, tick] = useState(0);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [plain, setPlain] = useState(false);
  useEffect(() => {
    const f = () => tick((n) => n + 1);
    subs.add(f);
    return () => void subs.delete(f);
  }, []);
  const cur = queue[0];
  useEffect(() => {
    setErr('');
    setPlain(false);
  }, [cur]);
  if (!cur) return null;
  const canApprove = !!session && (session.isAdmin || session.canSudo || !!session.isRoot);
  const done = (approved: boolean) => {
    queue = queue.filter((x) => x !== cur);
    cur.settle({ approved });
    emit();
    if (approved) window.setTimeout(reload, 150);
  };
  const approve = async () => {
    setBusy(true);
    setErr('');
    try {
      await call('plugins.network.approve', { plugin: cur.plugin.id, host: cur.host, scheme: cur.scheme }, { admin: true });
      done(true);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const http = cur.scheme === 'http';
  return (
    <Dialog
      open
      onClose={() => done(false)}
      title={t('network.title', { plugin: cur.plugin.name })}
      description={t('network.text', { plugin: cur.plugin.name })}
      icon="globe"
      tone={http ? 'warn' : 'acc'}
      footer={
        canApprove ? (
          <>
            <Button variant="ghost" onClick={() => done(false)}>{t('network.deny')}</Button>
            <Button variant="primary" disabled={busy || (http && !plain)} onClick={() => void approve()}>{t('network.allow')}</Button>
          </>
        ) : (
          <Button variant="primary" onClick={() => done(false)}>{t('network.close')}</Button>
        )
      }
    >
      <div className="env-ask">
        <div className="env-ask-host"><code>{cur.host}</code><Badge tone={http ? 'warn' : 'ok'}>{http ? t('network.http') : t('network.https')}</Badge></div>
        <small>{t('network.exact')}</small>
        {http && (
          <label className="env-ask-plain">
            <input type="checkbox" checked={plain} onChange={(e) => setPlain(e.target.checked)} /> {t('network.plainOk')}
          </label>
        )}
        {!canApprove && <div className="env-note" role="status">{t('network.needAdmin')}</div>}
        {err && <div className="env-note env-note--err" role="alert">{err}</div>}
      </div>
    </Dialog>
  );
}
