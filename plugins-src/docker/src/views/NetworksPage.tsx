import { useMemo, useState } from 'react';
import { docker, errorText, resetEngine } from '../api/engine';
import { containerName } from '../api/format';
import { containers, networks } from '../api/resources';
import type { Container, NetworkInfo } from '../api/types';
import { t, tn } from '../i18n';
import { Badge, Button, Checkbox, ConfirmDialog, EmptyState, IconButton, Input, Select, Skeleton, toast } from '../kit';
import { navigate, useSearch } from '../router';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { groupContainers, matchesText } from './resources/bits';

/** Docker's own networks cannot be removed. */
const BUILTIN = new Set(['bridge', 'host', 'none']);
const CIDR = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$|^[0-9a-fA-F:]+\/\d{1,3}$/;
const IP = /^(\d{1,3}\.){3}\d{1,3}$|^[0-9a-fA-F:]+$/;

export function NetworksPage() {
  const { data, error, loading } = networks.use();
  const cts = containers.use().data ?? [];
  const global = useSearch();
  const [q, setQ] = useState('');
  const [creating, setCreating] = useState(false);
  const [connecting, setConnecting] = useState<string | null>(null);
  const [removing, setRemoving] = useState<NetworkInfo | null>(null);

  // The list call does not fill in attached containers, so derive them from the containers' own network settings.
  const attached = useMemo(() => groupContainers(cts, (c) => Object.keys(c.NetworkSettings?.Networks ?? {})), [cts]);
  const list = useMemo(() => [...(data ?? [])].sort((a, b) => Number(BUILTIN.has(b.Name)) - Number(BUILTIN.has(a.Name)) || a.Name.localeCompare(b.Name)), [data]);
  const visible = list.filter((n) => matchesText(q, n.Name, n.Driver, ...(n.IPAM?.Config ?? []).map((c) => c.Subnet)) && matchesText(global, n.Name, n.Driver));

  const header = (
    <PageHeader
      icon="net"
      hue="svc"
      title={t('nav.networks')}
      subtitle={data ? tn('res.networks.sub', { n: list.length }) : ''}
      actions={<Button variant="primary" icon="plus" onClick={() => setCreating(true)}>{t('res.networks.new')}</Button>}
    />
  );
  if (!data && error) return <>{header}<ErrorState error={error} onRetry={() => { resetEngine(); void networks.refresh(); }} /></>;
  if (!data && loading) return <>{header}<Skeleton lines={5} /></>;

  // Engine events can lag a moment behind an action: refresh now and once more shortly after.
  const refreshAll = () => {
    void networks.refresh();
    void containers.refresh();
    setTimeout(() => { void networks.refresh(); void containers.refresh(); }, 1200);
  };

  const disconnect = async (n: NetworkInfo, c: Container) => {
    try {
      await docker.post(`/networks/${n.Id}/disconnect`, undefined, { Container: c.Id, Force: false });
      toast.ok(t('res.networks.disconnected', { name: containerName(c), network: n.Name }));
      refreshAll();
    } catch (e) {
      toast.err(t('res.networks.disconnectFail', { name: containerName(c) }), errorText(e));
    }
  };

  const doRemove = async () => {
    const n = removing;
    if (!n) return;
    try {
      await docker.delete(`/networks/${n.Id}`);
      toast.ok(t('res.networks.removed', { name: n.Name }));
      refreshAll();
    } catch (e) {
      toast.err(t('res.networks.removeFail', { name: n.Name }), errorText(e));
    } finally {
      setRemoving(null);
    }
  };

  return (
    <>
      {header}
      {creating && <CreateNetwork onClose={() => setCreating(false)} onDone={refreshAll} />}
      <section className="dk-card">
        <div className="dk-bar1">
          <Input fieldClassName="dk-grow" icon="search" placeholder={t('res.networks.filter')} aria-label={t('res.networks.filter')} value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        {visible.length === 0 ? (
          <EmptyState icon="search" hue="svc" title={t('res.noMatch.title')} text={t('res.noMatch.text')} />
        ) : (
          <div className="dk-tablewrap">
            <table className="dk-table">
              <thead>
                <tr>
                  <th>{t('res.col.name')}</th>
                  <th>{t('res.networks.col.driver')}</th>
                  <th className="dk-hide-sm">{t('res.networks.col.scope')}</th>
                  <th className="dk-hide-md">{t('res.networks.col.subnet')}</th>
                  <th>{t('res.networks.col.attached')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {visible.map((n) => {
                  const users = attached.get(n.Name) ?? [];
                  const cfg = (n.IPAM?.Config ?? []).filter((c) => c.Subnet);
                  const builtin = BUILTIN.has(n.Name);
                  const opening = connecting === n.Id;
                  const free = cts.filter((c) => !users.includes(c) && c.HostConfig?.NetworkMode !== 'host' && c.HostConfig?.NetworkMode !== 'none');
                  return (
                    <NetRow
                      key={n.Id}
                      net={n}
                      cfg={cfg}
                      builtin={builtin}
                      users={users}
                      opening={opening}
                      free={free}
                      onToggle={() => setConnecting(opening ? null : n.Id)}
                      onDisconnect={(c) => void disconnect(n, c)}
                      onRemove={() => setRemoving(n)}
                      onConnected={() => { setConnecting(null); refreshAll(); }}
                    />
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        onConfirm={doRemove}
        title={t('res.networks.removeTitle', { name: removing?.Name ?? '' })}
        description={t('res.networks.removeDesc')}
        confirmLabel={t('common.remove')}
        confirmText={removing?.Name}
        icon="trash"
        danger
      />
    </>
  );
}

function NetRow({ net, cfg, builtin, users, opening, free, onToggle, onDisconnect, onRemove, onConnected }: {
  net: NetworkInfo;
  cfg: { Subnet?: string; Gateway?: string }[];
  builtin: boolean;
  users: Container[];
  opening: boolean;
  free: Container[];
  onToggle(): void;
  onDisconnect(c: Container): void;
  onRemove(): void;
  onConnected(): void;
}) {
  const [pick, setPick] = useState('');
  const [busy, setBusy] = useState(false);
  const connect = async () => {
    const c = free.find((x) => x.Id === pick);
    if (!c) return;
    setBusy(true);
    try {
      await docker.post(`/networks/${net.Id}/connect`, undefined, { Container: c.Id });
      toast.ok(t('res.networks.connected', { name: containerName(c), network: net.Name }));
      setPick('');
      onConnected();
    } catch (e) {
      toast.err(t('res.networks.connectFail', { name: containerName(c) }), errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const canConnect = net.Name !== 'host' && net.Name !== 'none';
  return (
    <>
      <tr className={opening ? 'dk-sel' : undefined}>
        <td>
          <span className="dk-repo">{net.Name}</span>
          {builtin && <> <Badge tone="neutral">{t('res.networks.builtin')}</Badge></>}
          {net.Internal && <> <Badge tone="warn">{t('res.networks.internal')}</Badge></>}
        </td>
        <td><span className="dk-tag">{net.Driver}</span></td>
        <td className="dk-hide-sm dk-muted">{net.Scope}</td>
        <td className="dk-hide-md">
          {cfg.length ? cfg.map((c) => <div key={c.Subnet} className="dk-sub"><code>{c.Subnet}</code>{c.Gateway && <span className="dk-muted"> {t('res.networks.gw', { ip: c.Gateway })}</span>}</div>) : <span className="dk-muted">–</span>}
        </td>
        <td>
          <div className="dk-used">
            {users.length === 0 && <small className="dk-muted">{t('res.networks.none')}</small>}
            {users.map((c) => (
              <span key={c.Id} className="dk-uchip dk-uchip--x">
                <button type="button" onClick={() => navigate({ view: 'container', id: c.Id })}>{containerName(c)}</button>
                {canConnect && <button type="button" className="dk-x" aria-label={t('res.networks.disconnect', { name: containerName(c) })} title={t('res.networks.disconnect', { name: containerName(c) })} onClick={() => onDisconnect(c)}>×</button>}
              </span>
            ))}
          </div>
        </td>
        <td>
          <div className="dk-act">
            {canConnect && <IconButton icon="link" size="sm" variant="ghost" label={t('res.networks.connect')} onClick={onToggle} />}
            <IconButton icon="trash" size="sm" variant="ghost" label={builtin ? t('res.networks.builtinHint') : t('common.remove')} disabled={builtin} onClick={onRemove} />
          </div>
        </td>
      </tr>
      {opening && (
        <tr className="dk-exp">
          <td colSpan={6}>
            <div className="dk-exp-in">
              <div className="dk-exp-h"><b>{t('res.networks.connectTo', { network: net.Name })}</b></div>
              {free.length === 0 ? (
                <p className="dk-muted">{t('res.networks.allConnected')}</p>
              ) : (
                <div className="dk-form">
                  <Select
                    label={t('res.networks.container')}
                    value={pick}
                    onChange={setPick}
                    options={[{ value: '', label: t('res.networks.pick') }, ...free.map((c) => ({ value: c.Id, label: containerName(c) }))]}
                  />
                  <Button variant="primary" icon="link" disabled={!pick} loading={busy} onClick={() => void connect()}>{t('res.networks.connect')}</Button>
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function CreateNetwork({ onClose, onDone }: { onClose(): void; onDone(): void }) {
  const [name, setName] = useState('');
  const [driver, setDriver] = useState('bridge');
  const [subnet, setSubnet] = useState('');
  const [gateway, setGateway] = useState('');
  const [parent, setParent] = useState('');
  const [internal, setInternal] = useState(false);
  const [attachable, setAttachable] = useState(false);
  const [busy, setBusy] = useState(false);
  const nameBad = name !== '' && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/.test(name);
  const subnetBad = subnet !== '' && !CIDR.test(subnet.trim());
  const gwBad = gateway !== '' && !IP.test(gateway.trim());
  const needSubnet = driver === 'macvlan';
  const ok = name !== '' && !nameBad && !subnetBad && !gwBad && (!needSubnet || (subnet !== '' && parent !== ''));
  const submit = async () => {
    setBusy(true);
    try {
      const body: Record<string, unknown> = { Name: name.trim(), Driver: driver, Internal: internal, Attachable: attachable, CheckDuplicate: true };
      if (subnet.trim()) body.IPAM = { Driver: 'default', Config: [{ Subnet: subnet.trim(), ...(gateway.trim() ? { Gateway: gateway.trim() } : {}) }] };
      if (driver === 'macvlan' && parent.trim()) body.Options = { parent: parent.trim() };
      await docker.post('/networks/create', undefined, body);
      toast.ok(t('res.networks.created', { name }));
      onDone();
      onClose();
    } catch (e) {
      toast.err(t('res.networks.createFail'), errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="dk-card" aria-label={t('res.networks.new')}>
      <div className="dk-card-h">
        <h3>{t('res.networks.new')}</h3>
        <Button size="sm" variant="ghost" icon="close" onClick={onClose}>{t('common.close')}</Button>
      </div>
      <div className="dk-form dk-form--col" onKeyDown={(e) => { if (e.key === 'Enter' && ok && !busy) { e.preventDefault(); void submit(); } }}>
        <div className="dk-fields">
        <Input mono label={t('res.col.name')} value={name} onChange={(e) => setName(e.target.value)} error={nameBad ? t('res.networks.nameBad') : undefined} autoFocus />
        <Select label={t('res.networks.col.driver')} value={driver} onChange={setDriver} options={[{ value: 'bridge', label: t('res.networks.bridge') }, { value: 'macvlan', label: t('res.networks.macvlan') }]} />
        <Input mono label={t('res.networks.subnet')} placeholder="172.30.0.0/24" value={subnet} onChange={(e) => setSubnet(e.target.value)} error={subnetBad ? t('res.networks.subnetBad') : undefined} />
        <Input mono label={t('res.networks.gateway')} placeholder="172.30.0.1" value={gateway} onChange={(e) => setGateway(e.target.value)} error={gwBad ? t('res.networks.gatewayBad') : undefined} />
        {driver === 'macvlan' && <Input mono label={t('res.networks.parent')} placeholder="eth0" value={parent} onChange={(e) => setParent(e.target.value)} hint={t('res.networks.parentHint')} />}
        </div>
      </div>
      <div className="dk-form dk-form--end">
        <Checkbox checked={internal} onChange={setInternal} label={t('res.networks.internalLabel')} />
        <Checkbox checked={attachable} onChange={setAttachable} label={t('res.networks.attachable')} />
        <Button variant="primary" icon="plus" loading={busy} disabled={!ok} onClick={() => void submit()}>{t('res.networks.create')}</Button>
      </div>
    </section>
  );
}
