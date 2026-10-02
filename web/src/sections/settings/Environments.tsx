import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, ConfirmDialog, Dialog, IconButton, Input, Segmented, Select, Skeleton, Textarea, toast } from '../../ui';
import { NameList } from './NameList';
import {
  ENV_KINDS, envsCreate, envsDelete, envsList, envsProbe, envsTest, envsUpdate,
  type EnvInput, type EnvKind, type EnvList, type EnvView, type ProbeResult,
} from './envs';
import './envs.css';

const errText = (e: unknown) => (e instanceof ApiError || e instanceof Error ? e.message : String(e));

/** Settings > Environments: remote Docker hosts that plugins can target. Admin only. */
export function EnvironmentsBlock({ disabled, onData }: { disabled?: boolean; onData?(d: EnvList | null): void }) {
  const t = useT('envs');
  const [data, setData] = useState<EnvList | null>(null);
  const [failed, setFailed] = useState('');
  const [wizard, setWizard] = useState<{ env?: EnvView } | null>(null);
  const [removing, setRemoving] = useState<EnvView | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const onDataRef = useRef(onData);
  onDataRef.current = onData;

  const load = useCallback(async () => {
    try {
      const d = await envsList();
      setData(d);
      setFailed('');
      onDataRef.current?.(d);
    } catch (e) {
      setFailed(errText(e));
    }
  }, []);
  useEffect(() => {
    if (disabled) return;
    void load();
    const id = window.setInterval(() => void load(), 30000);
    return () => window.clearInterval(id);
  }, [load, disabled]);

  const test = async (env: EnvView) => {
    setTesting(env.id);
    try {
      const st = await envsTest(env.id);
      toast[st.reachable ? 'ok' : 'err'](st.reachable ? t('test.ok', { name: env.name, ms: st.latencyMs }) : t('test.failed', { name: env.name }), st.error);
      await load();
    } catch (e) {
      toast.err(t('test.failed', { name: env.name }), errText(e));
    } finally {
      setTesting(null);
    }
  };
  const remove = async (env: EnvView) => {
    await envsDelete(env.id);
    toast.ok(t('remove.done', { name: env.name }));
    await load();
  };

  if (failed) {
    return (
      <div className="st-lock" role="alert">
        <div className="grow"><b>{t('list.failed')}</b><small>{failed}</small></div>
        <Button icon="refresh" onClick={() => void load()}>{t('list.retry')}</Button>
      </div>
    );
  }
  if (!data) return <Skeleton lines={3} />;
  return (
    <>
      {data.envs.length === 0 && <div className="env-empty"><b>{t('list.empty')}</b><small>{t('list.emptyText')}</small></div>}
      {data.envs.map((e) => {
        const st = e.status;
        return (
          <div className="st-host env-row" key={e.id}>
            <span className="dot" style={{ background: !st ? 'var(--ink3)' : st.reachable ? 'var(--ok)' : 'var(--err)' }} />
            <div className="grow">
              <b>{e.name}</b>
              <small>
                {t(`kind.${e.kind}.name`)} · <span className="mono">{e.address}</span>
                {st?.reachable && st.engineVersion ? ` · Docker ${st.engineVersion}` : ''}
                {st?.reachable ? ` · ${st.latencyMs} ms` : ''}
                {e.kind === 'ervisio' && e.pairedWith ? ` · ${e.pairedWith}` : ''}
                {e.access.mode === 'restricted' ? ` · ${t('access.restrictedShort')}` : ''}
                {e.kind === 'tcp-tls' && e.insecure ? ` · ${t('kind.tcp-tls.insecureTag')}` : ''}
                {e.kind === 'tcp-tls' && e.skipVerify ? ` · ${t('kind.tcp-tls.skipTag')}` : ''}
              </small>
              {st && !st.reachable && st.error && <small className="env-err">{st.error}</small>}
            </div>
            <Badge tone={!st ? 'neutral' : st.reachable ? 'ok' : 'err'}>{!st ? t('status.unknown') : st.reachable ? t('status.online') : t('status.offline')}</Badge>
            <IconButton icon="refresh" label={t('test.action', { name: e.name })} disabled={disabled || testing === e.id} onClick={() => void test(e)} />
            <IconButton icon="edit" label={t('edit.action', { name: e.name })} disabled={disabled} onClick={() => setWizard({ env: e })} />
            <IconButton icon="trash" label={t('remove.action', { name: e.name })} disabled={disabled} onClick={() => setRemoving(e)} />
          </div>
        );
      })}
      <div style={{ padding: '10px 0' }}>
        <Button icon="plus" disabled={disabled} onClick={() => setWizard({})}>{t('add.action')}</Button>
      </div>
      {wizard && <EnvWizard env={wizard.env} onClose={() => setWizard(null)} onDone={() => { setWizard(null); void load(); }} />}
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        onConfirm={() => removing && remove(removing)}
        title={t('remove.title', { name: removing?.name ?? '' })}
        description={removing?.kind === 'ervisio' ? t('remove.textPaired') : t('remove.text')}
        confirmLabel={t('remove.confirm')}
        icon="trash"
      />
    </>
  );
}

/* ------------------------------------------------------------------------------------------------ */

type Trust = 'ca' | 'pin' | 'skip';
interface Form {
  name: string;
  kind: EnvKind;
  address: string;
  user: string;
  socketPath: string;
  insecure: boolean;
  trust: Trust;
  ca: string;
  clientCert: string;
  clientKey: string;
  sshKey: string;
  passphrase: string;
  agentSecret: string;
  token: string;
  fingerprint: string;
  hostKey: string;
  confirmed: boolean;
  users: string[];
  groups: string[];
  restricted: boolean;
}

function initial(env?: EnvView): Form {
  return {
    name: env?.name ?? '',
    kind: env?.kind ?? 'tcp-tls',
    address: env?.address ?? '',
    user: env?.user ?? '',
    socketPath: env?.socketPath ?? '',
    insecure: !!env?.insecure,
    trust: env?.skipVerify ? 'skip' : env?.fingerprint && env.kind === 'tcp-tls' ? 'pin' : 'ca',
    ca: env?.ca ?? '',
    clientCert: env?.clientCert ?? '',
    clientKey: '',
    sshKey: '',
    passphrase: '',
    agentSecret: '',
    token: '',
    fingerprint: env?.fingerprint ?? '',
    hostKey: env?.hostKey ?? '',
    confirmed: !!env,
    users: env?.access.users ?? [],
    groups: env?.access.groups ?? [],
    restricted: env?.access.mode === 'restricted',
  };
}

function toInput(f: Form, editing: boolean): EnvInput {
  const base: EnvInput = {
    name: f.name.trim(), kind: f.kind, address: f.address.trim(),
    access: { mode: f.restricted ? 'restricted' : 'all', users: f.users, groups: f.groups },
  };
  switch (f.kind) {
    case 'tcp-tls':
      return {
        ...base, insecure: f.insecure, skipVerify: !f.insecure && f.trust === 'skip',
        ca: !f.insecure && f.trust === 'ca' ? f.ca.trim() : '', clientCert: f.insecure ? '' : f.clientCert.trim(),
        fingerprint: !f.insecure && f.trust === 'pin' ? f.fingerprint : '',
        ...(f.clientKey ? { clientKey: f.clientKey.trim() } : {}),
      };
    case 'ssh':
      return { ...base, user: f.user.trim(), socketPath: f.socketPath.trim(), hostKey: f.hostKey, ...(f.sshKey ? { sshKey: f.sshKey.trim() } : {}), ...(f.passphrase ? { passphrase: f.passphrase } : {}) };
    case 'portainer-agent':
      return { ...base, fingerprint: f.fingerprint, ...(f.agentSecret ? { agentSecret: f.agentSecret } : {}) };
    case 'ervisio':
      return { ...base, insecure: f.insecure, fingerprint: f.fingerprint, ...(editing ? {} : { token: f.token.trim() }) };
  }
}

const STEPS = ['kind', 'connection', 'access'] as const;

function EnvWizard({ env, onClose, onDone }: { env?: EnvView; onClose(): void; onDone(): void }) {
  const t = useT('envs');
  const editing = !!env;
  const [f, setF] = useState<Form>(() => initial(env));
  const [step, setStep] = useState(0);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [probing, setProbing] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<Form>) => setF((x) => ({ ...x, ...patch }));
  const changedAddress = editing && f.address.trim() !== env!.address;

  const needsPin = f.kind === 'ssh' || f.kind === 'portainer-agent' || (f.kind === 'ervisio' && f.address.startsWith('https://')) || (f.kind === 'tcp-tls' && !f.insecure && f.trust === 'pin');

  const doProbe = async () => {
    setProbing(true);
    setErr('');
    setProbe(null);
    try {
      const r = await envsProbe({ kind: f.kind, address: f.address.trim(), user: f.user.trim() || 'root', insecure: f.insecure });
      setProbe(r);
      set({ fingerprint: r.fingerprint, hostKey: r.hostKey ?? '', confirmed: false });
    } catch (e) {
      setErr(errText(e));
    } finally {
      setProbing(false);
    }
  };

  const addrOk = f.address.trim().length > 0 && (f.kind !== 'ssh' || f.user.trim().length > 0);
  const pinOk = !needsPin || (!!f.fingerprint && f.confirmed && (f.kind !== 'ssh' || !!f.hostKey));
  const kindOk = f.name.trim().length > 0;
  const credOk = (() => {
    switch (f.kind) {
      case 'ssh': return editing || f.sshKey.trim().length > 0;
      case 'ervisio': return editing || f.token.trim().length > 0;
      default: return true;
    }
  })();
  const stepOk = [kindOk, addrOk && pinOk, credOk];

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editing && step < STEPS.length - 1) {
      if (stepOk[step]) setStep(step + 1);
      return;
    }
    if (!stepOk.every(Boolean) && !editing) return;
    setBusy(true);
    setErr('');
    try {
      const input = toInput(f, editing);
      const v = editing ? await envsUpdate(env!.id, input) : await envsCreate(input);
      if (v.status && !v.status.reachable) toast.err(t('save.savedButOffline', { name: v.name }), v.status.error);
      else toast.ok(t(editing ? 'save.updated' : 'save.added', { name: v.name }));
      onDone();
    } catch (e2) {
      setErr(errText(e2));
    } finally {
      setBusy(false);
    }
  };

  const last = editing || step === STEPS.length - 1;
  return (
    <Dialog
      open
      onClose={onClose}
      size="lg"
      icon="server"
      title={editing ? t('edit.title', { name: env!.name }) : t('add.title')}
      description={editing ? undefined : t(`add.step.${STEPS[step]}`, { n: step + 1, total: STEPS.length })}
      onSubmit={(e) => void submit(e)}
      footer={
        <>
          {err && <span className="env-foot-err" role="alert">{err}</span>}
          <Button variant="ghost" onClick={onClose}>{t('cancel')}</Button>
          {!editing && step > 0 && <Button variant="ghost" onClick={() => { setStep(step - 1); setErr(''); }}>{t('back')}</Button>}
          <Button type="submit" variant="primary" disabled={busy || (editing ? !(addrOk && (!changedAddress || pinOk) && kindOk) : !stepOk[step])}>
            {busy ? t('saving') : last ? (editing ? t('edit.save') : t('add.action')) : t('next')}
          </Button>
        </>
      }
    >
      <div className="env-form">
        {(editing || step === 0) && <KindStep f={f} set={set} editing={editing} />}
        {(editing || step === 1) && (
          <ConnectionStep f={f} set={set} probe={probe} probing={probing} onProbe={() => void doProbe()} editing={editing} changedAddress={changedAddress} needsPin={needsPin} />
        )}
        {(editing || step === 2) && <CredentialsStep f={f} set={set} editing={editing} env={env} />}
      </div>
    </Dialog>
  );
}

function KindStep({ f, set, editing }: { f: Form; set(p: Partial<Form>): void; editing: boolean }) {
  const t = useT('envs');
  return (
    <>
      <Input data-autofocus label={t('field.name')} value={f.name} maxLength={60} onChange={(e) => set({ name: e.target.value })} placeholder={t('field.namePlaceholder')} />
      {!editing && (
        <fieldset className="env-kinds">
          <legend>{t('field.kind')}</legend>
          {ENV_KINDS.map((k) => (
            <label key={k} className="env-kind" data-on={f.kind === k}>
              <input type="radio" name="env-kind" checked={f.kind === k} onChange={() => set({ kind: k, fingerprint: '', hostKey: '', confirmed: false, insecure: false })} />
              <span><b>{t(`kind.${k}.name`)}</b><small>{t(`kind.${k}.desc`)}</small></span>
            </label>
          ))}
        </fieldset>
      )}
    </>
  );
}

function ConnectionStep({ f, set, probe, probing, onProbe, editing, changedAddress, needsPin }: {
  f: Form; set(p: Partial<Form>): void; probe: ProbeResult | null; probing: boolean; onProbe(): void; editing: boolean; changedAddress: boolean; needsPin: boolean;
}) {
  const t = useT('envs');
  const k = f.kind;
  const probeLabel = k === 'ssh' ? 'ssh' : 'tls';
  const showProbe = needsPin;
  return (
    <>
      {editing && <h4 className="env-sec">{t('section.connection')}</h4>}
      <Input
        label={k === 'ervisio' ? t('field.serverUrl') : t('field.address')}
        mono
        icon="server"
        value={f.address}
        onChange={(e) => set({ address: e.target.value, confirmed: false })}
        placeholder={t(`kind.${k}.addressPlaceholder`)}
        hint={t(`kind.${k}.addressHint`)}
      />
      {k === 'ssh' && <Input label={t('field.sshUser')} icon="user" value={f.user} onChange={(e) => set({ user: e.target.value, confirmed: false })} placeholder="docker" hint={t('kind.ssh.userHint')} />}
      {k === 'tcp-tls' && (
        <>
          <label className="env-check">
            <input type="checkbox" checked={f.insecure} onChange={(e) => set({ insecure: e.target.checked, confirmed: false })} /> {t('kind.tcp-tls.insecure')}
          </label>
          {f.insecure ? (
            <div className="env-warn" role="note">{t('kind.tcp-tls.insecureWarn')}</div>
          ) : (
            <>
              <Segmented
                aria-label={t('field.trust')}
                value={f.trust}
                onChange={(v) => set({ trust: v, confirmed: false })}
                options={[{ value: 'ca', label: t('trust.ca') }, { value: 'pin', label: t('trust.pin') }, { value: 'skip', label: t('trust.skip') }]}
              />
              {f.trust === 'ca' && <PemField label={t('field.ca')} hint={t('field.caHint')} value={f.ca} onChange={(v) => set({ ca: v })} />}
              {f.trust === 'skip' && <div className="env-warn" role="note">{t('trust.skipWarn')}</div>}
              {f.trust === 'pin' && <small className="env-hint">{t('trust.pinHint')}</small>}
            </>
          )}
        </>
      )}
      {k === 'ervisio' && (
        <label className="env-check">
          <input type="checkbox" checked={f.insecure} onChange={(e) => set({ insecure: e.target.checked, confirmed: false })} /> {t('kind.ervisio.plain')}
        </label>
      )}
      {k === 'ervisio' && f.insecure && <div className="env-warn" role="note">{t('kind.ervisio.plainWarn')}</div>}
      {showProbe && (
        <div className="env-probe">
          <div className="env-probe-row">
            <Button icon="shield" disabled={probing || !f.address.trim() || (k === 'ssh' && !f.user.trim())} onClick={onProbe}>{probing ? t('probe.checking') : t(`probe.${probeLabel}`)}</Button>
            {editing && f.fingerprint && !probe && !changedAddress && <small className="env-hint">{t('probe.pinned')}</small>}
          </div>
          {f.fingerprint && probe && (
            <div className="env-fp">
              <small>{t(k === 'ssh' ? 'probe.sshFound' : 'probe.tlsFound')}</small>
              <code>{f.fingerprint}</code>
              <small className="env-hint">{t(`probe.verify.${k}`)}</small>
              <label className="env-check">
                <input type="checkbox" checked={f.confirmed} onChange={(e) => set({ confirmed: e.target.checked })} /> {t('probe.confirm')}
              </label>
            </div>
          )}
          {f.fingerprint && !probe && editing && !changedAddress && (
            <div className="env-fp">
              <small>{t('probe.current')}</small>
              <code>{f.fingerprint}</code>
            </div>
          )}
          {f.kind === 'ervisio' && probe?.plain && <small className="env-hint">{t('probe.plainServer')}</small>}
        </div>
      )}
      {k === 'ervisio' && !f.address.startsWith('https://') && f.address.startsWith('http://') && !probe && <small className="env-hint">{t('probe.localHttp')}</small>}
    </>
  );
}

function CredentialsStep({ f, set, editing, env }: { f: Form; set(p: Partial<Form>): void; editing: boolean; env?: EnvView }) {
  const t = useT('envs');
  const k = f.kind;
  const has = env?.hasSecrets ?? {};
  return (
    <>
      {editing && <h4 className="env-sec">{t('section.credentials')}</h4>}
      {k === 'tcp-tls' && !f.insecure && (
        <>
          <PemField label={t('field.clientCert')} hint={t('field.clientCertHint')} value={f.clientCert} onChange={(v) => set({ clientCert: v })} />
          <PemField label={t('field.clientKey')} hint={editing && has.clientKey ? t('field.keepSecret') : t('field.clientKeyHint')} secret value={f.clientKey} onChange={(v) => set({ clientKey: v })} />
        </>
      )}
      {k === 'tcp-tls' && f.insecure && <small className="env-hint">{t('kind.tcp-tls.noCreds')}</small>}
      {k === 'ssh' && (
        <>
          <PemField label={t('field.sshKey')} hint={editing && has.sshKey ? t('field.keepSecret') : t('field.sshKeyHint')} secret value={f.sshKey} onChange={(v) => set({ sshKey: v })} />
          <Input label={t('field.passphrase')} type="password" autoComplete="off" value={f.passphrase} onChange={(e) => set({ passphrase: e.target.value })} hint={editing && has.passphrase ? t('field.keepSecret') : t('field.passphraseHint')} />
          <Input label={t('field.socketPath')} mono value={f.socketPath} onChange={(e) => set({ socketPath: e.target.value })} placeholder="/var/run/docker.sock" hint={t('field.socketPathHint')} />
        </>
      )}
      {k === 'portainer-agent' && (
        <Input label={t('field.agentSecret')} type="password" autoComplete="off" value={f.agentSecret} onChange={(e) => set({ agentSecret: e.target.value })} hint={editing && has.agentSecret ? t('field.keepSecret') : t('field.agentSecretHint')} />
      )}
      {k === 'ervisio' && !editing && (
        <Input label={t('field.token')} mono value={f.token} onChange={(e) => set({ token: e.target.value })} placeholder="ept_…" hint={t('field.tokenHint')} autoComplete="off" />
      )}
      {k === 'ervisio' && editing && <small className="env-hint">{t('field.pairedHint', { name: env?.pairedWith ?? '' })}</small>}

      <h4 className="env-sec">{t('section.access')}</h4>
      <Select
        label={t('access.who')}
        value={f.restricted ? 'restricted' : 'all'}
        onChange={(v) => set({ restricted: v === 'restricted' })}
        options={[{ value: 'all', label: t('access.all') }, { value: 'restricted', label: t('access.restricted') }]}
        hint={t('access.hint')}
      />
      {f.restricted && (
        <div className="env-names">
          <label className="env-lab">{t('access.users')}</label>
          <NameList names={f.users} label={t('access.users')} placeholder={t('access.usersHint')} onChange={(users) => set({ users })} />
          <label className="env-lab">{t('access.groups')}</label>
          <NameList names={f.groups} label={t('access.groups')} placeholder={t('access.groupsHint')} onChange={(groups) => set({ groups })} />
        </div>
      )}
    </>
  );
}

/** A PEM text area with "Choose file": the file is read in the browser and sent over the session like the text. */
function PemField({ label, hint, value, onChange, secret }: { label: string; hint?: string; value: string; onChange(v: string): void; secret?: boolean }) {
  const t = useT('envs');
  const file = useRef<HTMLInputElement>(null);
  const pick = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const fl = e.target.files?.[0];
    e.target.value = '';
    if (!fl) return;
    if (fl.size > 64 << 10) {
      toast.err(t('pem.tooBig', { name: fl.name }));
      return;
    }
    onChange(await fl.text());
  };
  return (
    <div className="env-pem">
      <Textarea
        label={label}
        hint={hint}
        mono
        rows={4}
        spellCheck={false}
        autoComplete="off"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={secret ? '-----BEGIN PRIVATE KEY-----' : '-----BEGIN CERTIFICATE-----'}
      />
      <input ref={file} type="file" hidden onChange={(e) => void pick(e)} />
      <Button icon="upload" onClick={() => file.current?.click()}>{t('pem.choose')}</Button>
    </div>
  );
}
