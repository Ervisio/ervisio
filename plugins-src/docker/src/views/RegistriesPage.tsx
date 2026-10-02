import { useState } from 'react';
import { errorText } from '../api/engine';
import { DOCKER_HUB, normalizeServer, testRegistry } from '../api/registries';
import { useFile, type Registry } from '../settings';
import { t } from '../i18n';
import { Badge, Button, ConfirmDialog, EmptyState, Icon, IconButton, Input, Skeleton, toast } from '../kit';
import { PageHeader } from '../ui/PageHeader';

const PRESETS = [DOCKER_HUB, 'ghcr.io', 'quay.io'];
const newId = () => Math.random().toString(36).slice(2, 10);

type Check = { state: 'busy' } | { state: 'ok' } | { state: 'fail'; message: string };

export function RegistriesPage() {
  const [file, update, loaded] = useFile('registries');
  const list = file.registries ?? [];
  const [editing, setEditing] = useState<Registry | 'new' | null>(null);
  const [removing, setRemoving] = useState<Registry | null>(null);
  const [checks, setChecks] = useState<Record<string, Check>>({});

  const test = async (r: Registry) => {
    setChecks((c) => ({ ...c, [r.id]: { state: 'busy' } }));
    try {
      await testRegistry(r);
      setChecks((c) => ({ ...c, [r.id]: { state: 'ok' } }));
    } catch (e) {
      setChecks((c) => ({ ...c, [r.id]: { state: 'fail', message: errorText(e) } }));
    }
  };

  const save = async (r: Registry) => {
    const exists = list.some((x) => x.id === r.id);
    try {
      await update({ registries: exists ? list.map((x) => (x.id === r.id ? r : x)) : [...list, r] });
      toast.ok(t('res.reg.saved', { server: r.server }));
      setChecks((c) => { const n = { ...c }; delete n[r.id]; return n; });
      setEditing(null);
    } catch (e) {
      toast.err(t('res.reg.saveFail'), errorText(e));
    }
  };

  const remove = async () => {
    const r = removing;
    if (!r) return;
    try {
      await update({ registries: list.filter((x) => x.id !== r.id) });
      toast.ok(t('res.reg.removed', { server: r.server }));
    } catch (e) {
      toast.err(t('res.reg.saveFail'), errorText(e));
    } finally {
      setRemoving(null);
    }
  };

  return (
    <>
      <PageHeader
        icon="key"
        hue="usr"
        title={t('nav.registries')}
        subtitle={t('res.reg.sub')}
        actions={<Button variant="primary" icon="plus" onClick={() => setEditing('new')}>{t('res.reg.add')}</Button>}
      />
      <p className="dk-note dk-note--box"><Icon name="lock" />{t('res.reg.warn')}</p>
      {editing && <RegistryForm key={editing === 'new' ? 'new' : editing.id} initial={editing === 'new' ? undefined : editing} taken={list.filter((x) => editing === 'new' || x.id !== editing.id).map((x) => normalizeServer(x.server))} onCancel={() => setEditing(null)} onSave={save} />}
      {!loaded ? (
        <Skeleton lines={3} />
      ) : list.length === 0 && !editing ? (
        <EmptyState icon="key" hue="usr" title={t('res.reg.empty.title')} text={t('res.reg.empty.text')} action={<Button variant="primary" icon="plus" onClick={() => setEditing('new')}>{t('res.reg.add')}</Button>} />
      ) : (
        <div className="dk-reglist">
          {list.map((r) => {
            const c = checks[r.id];
            return (
              <div key={r.id} className="dk-reg">
                <span className="dk-reg-ic"><Icon name="key" /></span>
                <div className="dk-reg-tx">
                  <b>{normalizeServer(r.server) === DOCKER_HUB ? t('res.reg.hub') : r.server}</b>
                  <span className="dk-muted">{normalizeServer(r.server) === DOCKER_HUB ? `${DOCKER_HUB}, ` : ''}{t('res.reg.user', { name: r.username })}</span>
                </div>
                <div className="dk-reg-st" aria-live="polite">
                  {c?.state === 'ok' && <Badge tone="ok" dot>{t('res.reg.ok')}</Badge>}
                  {c?.state === 'fail' && <span className="dk-fail" title={c.message}><Icon name="alert" />{c.message.length > 70 ? c.message.slice(0, 70) + '…' : c.message}</span>}
                </div>
                <div className="dk-act">
                  <Button size="sm" icon="shield" loading={c?.state === 'busy'} onClick={() => void test(r)}>{t('res.reg.test')}</Button>
                  <IconButton icon="edit" size="sm" variant="ghost" label={t('common.edit')} onClick={() => setEditing(r)} />
                  <IconButton icon="trash" size="sm" variant="ghost" label={t('common.remove')} onClick={() => setRemoving(r)} />
                </div>
              </div>
            );
          })}
        </div>
      )}
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        onConfirm={remove}
        title={t('res.reg.removeTitle', { server: removing?.server ?? '' })}
        description={t('res.reg.removeDesc')}
        confirmLabel={t('common.remove')}
        icon="trash"
        danger
      />
    </>
  );
}

function RegistryForm({ initial, taken, onCancel, onSave }: { initial?: Registry; taken: string[]; onCancel(): void; onSave(r: Registry): Promise<void> }) {
  const [server, setServer] = useState(initial?.server ?? DOCKER_HUB);
  const [username, setUsername] = useState(initial?.username ?? '');
  const [password, setPassword] = useState(initial?.password ?? '');
  const [show, setShow] = useState(false);
  const [check, setCheck] = useState<Check | null>(null);
  const [busy, setBusy] = useState(false);
  const host = normalizeServer(server);
  const dup = taken.includes(host);
  const hostBad = server.trim() === '' || /\s/.test(server.trim());
  const ok = !hostBad && !dup && username.trim() !== '' && password !== '';
  const save = () => { setBusy(true); void onSave(draft()).finally(() => setBusy(false)); };
  const draft = (): Registry => ({ id: initial?.id ?? newId(), server: host, username: username.trim(), password });
  return (
    <section className="dk-card" aria-label={initial ? t('res.reg.edit') : t('res.reg.add')}>
      <div className="dk-card-h">
        <h3>{initial ? t('res.reg.edit') : t('res.reg.add')}</h3>
        <Button size="sm" variant="ghost" icon="close" onClick={onCancel}>{t('common.cancel')}</Button>
      </div>
      <div className="dk-form dk-form--col" onKeyDown={(e) => { if (e.key === 'Enter' && ok && !busy) { e.preventDefault(); save(); } }}>
        <div className="dk-chips-row">
          {PRESETS.map((p) => (
            <button key={p} type="button" className={`dk-preset${host === p ? ' dk-preset--on' : ''}`} onClick={() => setServer(p)}>{p === DOCKER_HUB ? t('res.reg.hub') : p}</button>
          ))}
        </div>
        <div className="dk-fields">
          <Input mono label={t('res.reg.server')} placeholder="registry.example.com:5000" value={server} onChange={(e) => setServer(e.target.value)} error={dup ? t('res.reg.dup') : hostBad ? t('res.reg.serverBad') : undefined} hint={t('res.reg.serverHint')} />
          <Input label={t('res.reg.username')} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
          <Input
            label={t('res.reg.password')}
            type={show ? 'text' : 'password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            hint={t('res.reg.passwordHint')}
            end={<IconButton icon={show ? 'eyeoff' : 'eye'} size="sm" variant="ghost" label={show ? t('res.reg.hide') : t('res.reg.show')} onClick={() => setShow((v) => !v)} />}
          />
        </div>
        <div className="dk-form dk-form--end">
          <Button type="button" icon="shield" disabled={!ok} loading={check?.state === 'busy'} onClick={() => { setCheck({ state: 'busy' }); testRegistry(draft()).then(() => setCheck({ state: 'ok' }), (e) => setCheck({ state: 'fail', message: errorText(e) })); }}>{t('res.reg.test')}</Button>
          {check?.state === 'ok' && <Badge tone="ok" dot>{t('res.reg.ok')}</Badge>}
          {check?.state === 'fail' && <span className="dk-fail"><Icon name="alert" />{check.message}</span>}
          <span className="dk-grow" />
          <Button variant="primary" icon="check" disabled={!ok} loading={busy} onClick={save}>{t('common.save')}</Button>
        </div>
      </div>
    </section>
  );
}
