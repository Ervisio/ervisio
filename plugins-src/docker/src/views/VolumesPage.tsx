import { useMemo, useState } from 'react';
import { docker, errorText, resetEngine } from '../api/engine';
import { formatBytes, relativeTime } from '../api/format';
import { containers, diskUsage, volumes } from '../api/resources';
import type { VolumeInfo } from '../api/types';
import { t } from '../i18n';
import { Badge, Button, EmptyState, Icon, IconButton, Input, Segmented, Skeleton, toast, ConfirmDialog } from '../kit';
import { useSearch } from '../router';
import { DiskBar } from '../ui/DiskBar';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { CopyButton, groupContainers, isAnonymousVolume, matchesText, UsedBy } from './resources/bits';

type Filter = 'all' | 'used' | 'unused';

export function VolumesPage() {
  const { data, error, loading } = volumes.use();
  const cts = containers.use().data ?? [];
  const df = diskUsage.use().data;
  const global = useSearch();
  const [filter, setFilter] = useState<Filter>('all');
  const [q, setQ] = useState('');
  const [creating, setCreating] = useState(false);
  const [removing, setRemoving] = useState<VolumeInfo | null>(null);

  const usedBy = useMemo(() => groupContainers(cts, (c) => (c.Mounts ?? []).filter((m) => m.Type === 'volume' && m.Name).map((m) => m.Name!)), [cts]);
  const sizes = useMemo(() => new Map((df?.Volumes ?? []).map((v) => [v.Name, v.UsageData?.Size ?? -1])), [df]);
  const list = useMemo(() => [...(data ?? [])].sort((a, b) => a.Name.localeCompare(b.Name)), [data]);
  const isUsed = (v: VolumeInfo) => usedBy.has(v.Name);
  const counts = { all: list.length, used: list.filter(isUsed).length, unused: list.filter((v) => !isUsed(v)).length };
  const visible = list.filter((v) => (filter === 'all' || (filter === 'used') === isUsed(v)) && matchesText(q, v.Name, v.Driver, v.Mountpoint) && matchesText(global, v.Name, v.Driver, v.Mountpoint));
  const total = [...sizes.values()].reduce((a, s) => a + Math.max(0, s), 0);

  const header = (
    <PageHeader
      icon="database"
      hue="term"
      title={t('nav.volumes')}
      subtitle={data ? t('res.volumes.sub', { n: list.length, size: df ? formatBytes(total) : '–' }) : ''}
      actions={<Button variant="primary" icon="plus" onClick={() => setCreating(true)}>{t('res.volumes.new')}</Button>}
    />
  );
  if (!data && error) return <>{header}<ErrorState error={error} onRetry={() => { resetEngine(); void volumes.refresh(); }} /></>;
  if (!data && loading) return <>{header}<Skeleton height={90} style={{ borderRadius: 18 }} /><Skeleton lines={5} /></>;

  const doRemove = async () => {
    const v = removing;
    if (!v) return;
    try {
      await docker.delete(`/volumes/${encodeURIComponent(v.Name)}`);
      toast.ok(t('res.volumes.removed', { name: v.Name }));
      void volumes.refresh();
      void diskUsage.refresh();
    } catch (e) {
      toast.err(t('res.volumes.removeFail', { name: v.Name }), errorText(e));
    } finally {
      setRemoving(null);
    }
  };

  const segs = (['all', 'used', 'unused'] as Filter[]).map((f) => ({ value: f, label: `${t(`res.volumes.f.${f}`)} ${counts[f]}` }));
  const shown = (n: string) => (isAnonymousVolume(n) ? `${n.slice(0, 12)}…` : n);

  return (
    <>
      {header}
      <DiskBar />
      {creating && <CreateVolume onClose={() => setCreating(false)} />}
      <section className="dk-card">
        <div className="dk-bar1">
          <Input fieldClassName="dk-grow" icon="search" placeholder={t('res.volumes.filter')} aria-label={t('res.volumes.filter')} value={q} onChange={(e) => setQ(e.target.value)} />
          <Segmented options={segs} value={filter} onChange={(v) => setFilter(v as Filter)} aria-label={t('nav.volumes')} />
        </div>
        {list.length === 0 ? (
          <EmptyState icon="database" hue="term" title={t('res.volumes.empty.title')} text={t('res.volumes.empty.text')} action={<Button variant="primary" icon="plus" onClick={() => setCreating(true)}>{t('res.volumes.new')}</Button>} />
        ) : visible.length === 0 ? (
          <EmptyState icon="search" hue="term" title={t('res.noMatch.title')} text={t('res.noMatch.text')} />
        ) : (
          <div className="dk-tablewrap">
            <table className="dk-table">
              <thead>
                <tr>
                  <th>{t('res.col.name')}</th>
                  <th className="dk-hide-sm">{t('res.volumes.col.driver')}</th>
                  <th>{t('res.col.usedBy')}</th>
                  <th className="dk-num">{t('res.col.size')}</th>
                  <th className="dk-hide-md">{t('res.volumes.col.mountpoint')}</th>
                  <th className="dk-hide-md">{t('res.col.created')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {visible.map((v) => {
                  const s = sizes.get(v.Name);
                  const users = usedBy.get(v.Name) ?? [];
                  return (
                    <tr key={v.Name}>
                      <td>
                        <span className="dk-repo" title={v.Name}>{shown(v.Name)}</span>
                        {isAnonymousVolume(v.Name) && <> <Badge tone="neutral">{t('res.volumes.anonymous')}</Badge></>}
                      </td>
                      <td className="dk-hide-sm"><span className="dk-tag">{v.Driver}</span></td>
                      <td><UsedBy list={users} /></td>
                      <td className="dk-num">{s === undefined ? '–' : s < 0 ? (df ? '–' : '…') : formatBytes(s)}</td>
                      <td className="dk-hide-md">
                        <span className="dk-mp"><code title={v.Mountpoint}>{v.Mountpoint}</code><CopyButton text={v.Mountpoint} label={t('res.volumes.copyPath')} /></span>
                      </td>
                      <td className="dk-hide-md dk-muted">{v.CreatedAt ? relativeTime(v.CreatedAt) : '–'}</td>
                      <td>
                        <div className="dk-act">
                          <IconButton icon="trash" size="sm" variant="ghost" label={users.length ? t('res.volumes.inUseHint') : t('common.remove')} disabled={users.length > 0} onClick={() => setRemoving(v)} />
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        <p className="dk-note"><Icon name="info" />{t('res.volumes.note')}</p>
      </section>
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        onConfirm={doRemove}
        title={t('res.volumes.removeTitle', { name: removing ? shown(removing.Name) : '' })}
        description={t('res.volumes.removeDesc')}
        confirmLabel={t('common.remove')}
        confirmText={removing?.Name}
        icon="trash"
        danger
      />
    </>
  );
}

function CreateVolume({ onClose }: { onClose(): void }) {
  const [name, setName] = useState('');
  const [driver, setDriver] = useState('local');
  const [busy, setBusy] = useState(false);
  const bad = name !== '' && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/.test(name);
  const submit = async () => {
    setBusy(true);
    try {
      const v = await docker.post<VolumeInfo>('/volumes/create', undefined, { Name: name.trim() || undefined, Driver: driver.trim() || 'local' });
      toast.ok(t('res.volumes.created', { name: v?.Name ?? name }));
      void volumes.refresh();
      onClose();
    } catch (e) {
      toast.err(t('res.volumes.createFail'), errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="dk-card" aria-label={t('res.volumes.new')}>
      <div className="dk-card-h">
        <h3>{t('res.volumes.new')}</h3>
        <Button size="sm" variant="ghost" icon="close" onClick={onClose}>{t('common.close')}</Button>
      </div>
      <div className="dk-form dk-form--col" onKeyDown={(e) => { if (e.key === 'Enter' && !bad && !busy) { e.preventDefault(); void submit(); } }}>
        <div className="dk-fields dk-fields--2">
        <Input mono label={t('res.col.name')} placeholder={t('res.volumes.namePh')} value={name} onChange={(e) => setName(e.target.value)} error={bad ? t('res.volumes.nameBad') : undefined} hint={t('res.volumes.nameHint')} autoFocus />
        <Input mono label={t('res.volumes.col.driver')} value={driver} onChange={(e) => setDriver(e.target.value)} />
        </div>
        <div className="dk-form dk-form--end"><Button variant="primary" icon="plus" loading={busy} disabled={bad} onClick={() => void submit()}>{t('res.volumes.create')}</Button></div>
      </div>
    </section>
  );
}
