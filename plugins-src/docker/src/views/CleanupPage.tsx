import { useMemo, useState, type ReactNode } from 'react';
import { docker, errorText, resetEngine } from '../api/engine';
import { containerName, formatBytes, shortId } from '../api/format';
import { containers, diskUsage, images, networks, volumes } from '../api/resources';
import { t, tn } from '../i18n';
import { Button, Checkbox, ConfirmDialog, EmptyState, Icon, Segmented, Skeleton, toast } from '../kit';
import { navigate } from '../router';
import { DiskBar } from '../ui/DiskBar';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { isAnonymousVolume } from './resources/bits';
import { realTags } from './resources/imageRef';

type GroupKey = 'images' | 'containers' | 'volumes' | 'cache' | 'networks';
type ImageMode = 'dangling' | 'unused';
type VolumeMode = 'anonymous' | 'all';

interface Outcome {
  key: GroupKey;
  count: number;
  bytes: number;
  error?: string;
}

const BUILTIN = new Set(['bridge', 'host', 'none']);
const STOPPED = new Set(['exited', 'created', 'dead']);

export function CleanupPage() {
  const imgs = images.use();
  const cts = containers.use();
  const vols = volumes.use();
  const nets = networks.use();
  const df = diskUsage.use();
  const [on, setOn] = useState<Record<GroupKey, boolean>>({ images: true, containers: false, volumes: false, cache: true, networks: true });
  const [imageMode, setImageMode] = useState<ImageMode>('dangling');
  const [volumeMode, setVolumeMode] = useState<VolumeMode>('anonymous');
  const [confirm, setConfirm] = useState(false);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<Outcome[] | null>(null);

  const est = useMemo(() => {
    const inUseImages = new Set((cts.data ?? []).map((c) => c.ImageID));
    const unusedImgs = (imgs.data ?? []).filter((i) => !inUseImages.has(i.Id));
    const dangling = unusedImgs.filter((i) => realTags(i.RepoTags).length === 0);
    const stopped = (cts.data ?? []).filter((c) => STOPPED.has(c.State));
    const sizeRw = new Map((df.data?.Containers ?? []).map((c) => [c.Id, c.SizeRw ?? 0]));
    const volSize = new Map((df.data?.Volumes ?? []).map((v) => [v.Name, { size: Math.max(0, v.UsageData?.Size ?? 0), refs: v.UsageData?.RefCount ?? 1 }]));
    const idleVols = (vols.data ?? []).filter((v) => (volSize.get(v.Name)?.refs ?? 1) === 0 && !(cts.data ?? []).some((c) => (c.Mounts ?? []).some((m) => m.Name === v.Name)));
    const anonVols = idleVols.filter((v) => isAnonymousVolume(v.Name));
    const used = new Set((cts.data ?? []).flatMap((c) => Object.keys(c.NetworkSettings?.Networks ?? {})));
    const idleNets = (nets.data ?? []).filter((n) => !BUILTIN.has(n.Name) && !used.has(n.Name));
    const cache = (df.data?.BuildCache ?? []).filter((b) => !b.InUse);
    const sum = <T,>(a: T[], f: (x: T) => number) => a.reduce((s, x) => s + f(x), 0);
    return {
      images: { dangling, unused: unusedImgs, danglingBytes: sum(dangling, (i) => i.Size), unusedBytes: sum(unusedImgs, (i) => i.Size) },
      containers: { list: stopped, bytes: sum(stopped, (c) => sizeRw.get(c.Id) ?? 0) },
      volumes: { all: idleVols, anon: anonVols, allBytes: sum(idleVols, (v) => volSize.get(v.Name)?.size ?? 0), anonBytes: sum(anonVols, (v) => volSize.get(v.Name)?.size ?? 0) },
      cache: { count: cache.length, bytes: sum(cache, (b) => b.Size) },
      networks: { list: idleNets },
    };
  }, [imgs.data, cts.data, vols.data, nets.data, df.data]);

  const header = <PageHeader icon="broom" hue="log" title={t('nav.cleanup')} subtitle={t('res.cleanup.sub')} />;
  const err = imgs.error ?? cts.error;
  if (!imgs.data && err) return <>{header}<ErrorState error={err} onRetry={() => { resetEngine(); void images.refresh(); void containers.refresh(); }} /></>;
  if (!imgs.data || !cts.data) return <>{header}<Skeleton height={90} style={{ borderRadius: 18 }} /><Skeleton lines={5} /></>;

  const imgList = imageMode === 'dangling' ? est.images.dangling : est.images.unused;
  const imgBytes = imageMode === 'dangling' ? est.images.danglingBytes : est.images.unusedBytes;
  const volList = volumeMode === 'anonymous' ? est.volumes.anon : est.volumes.all;
  const volBytes = volumeMode === 'anonymous' ? est.volumes.anonBytes : est.volumes.allBytes;
  const items: Record<GroupKey, { count: number; bytes: number }> = {
    images: { count: imgList.length, bytes: imgBytes },
    containers: { count: est.containers.list.length, bytes: est.containers.bytes },
    volumes: { count: volList.length, bytes: volBytes },
    cache: { count: est.cache.count, bytes: est.cache.bytes },
    networks: { count: est.networks.list.length, bytes: 0 },
  };
  const active = (Object.keys(items) as GroupKey[]).filter((k) => on[k] && items[k].count > 0);
  const total = active.reduce((a, k) => a + items[k].bytes, 0);
  const sizesReady = !!df.data;

  const run = async () => {
    setConfirm(false);
    setRunning(true);
    const out: Outcome[] = [];
    const step = async (key: GroupKey, fn: () => Promise<{ count: number; bytes: number }>) => {
      try {
        out.push({ key, ...(await fn()) });
      } catch (e) {
        out.push({ key, count: 0, bytes: 0, error: errorText(e) });
      }
    };
    // Containers first: removing them makes more images and networks unused.
    if (active.includes('containers'))
      await step('containers', async () => {
        const r = await docker.post<{ ContainersDeleted?: string[] | null; SpaceReclaimed?: number }>('/containers/prune');
        return { count: r?.ContainersDeleted?.length ?? 0, bytes: r?.SpaceReclaimed ?? 0 };
      });
    if (active.includes('networks'))
      await step('networks', async () => {
        const r = await docker.post<{ NetworksDeleted?: string[] | null }>('/networks/prune');
        return { count: r?.NetworksDeleted?.length ?? 0, bytes: 0 };
      });
    if (active.includes('images'))
      await step('images', async () => {
        const r = await docker.post<{ ImagesDeleted?: unknown[] | null; SpaceReclaimed?: number }>('/images/prune', { filters: JSON.stringify({ dangling: [imageMode === 'dangling' ? 'true' : 'false'] }) });
        return { count: (r?.ImagesDeleted ?? []).filter((x) => (x as { Deleted?: string }).Deleted).length, bytes: r?.SpaceReclaimed ?? 0 };
      });
    if (active.includes('volumes'))
      await step('volumes', async () => {
        const r = await docker.post<{ VolumesDeleted?: string[] | null; SpaceReclaimed?: number }>('/volumes/prune', volumeMode === 'all' ? { filters: JSON.stringify({ all: ['true'] }) } : undefined);
        return { count: r?.VolumesDeleted?.length ?? 0, bytes: r?.SpaceReclaimed ?? 0 };
      });
    if (active.includes('cache'))
      await step('cache', async () => {
        const r = await docker.post<{ CachesDeleted?: string[] | null; SpaceReclaimed?: number }>('/build/prune', { all: 'true' });
        return { count: r?.CachesDeleted?.length ?? 0, bytes: r?.SpaceReclaimed ?? 0 };
      });
    setResult(out);
    setRunning(false);
    const failed = out.filter((o) => o.error);
    const freed = out.reduce((a, o) => a + o.bytes, 0);
    if (failed.length) toast.err(t('res.cleanup.partial'), failed[0].error);
    else toast.ok(t('res.cleanup.freed', { size: formatBytes(freed) }));
    void Promise.all([images.refresh(), containers.refresh(), volumes.refresh(), networks.refresh(), diskUsage.refresh()]);
  };

  const go = () => (active.includes('volumes') ? setConfirm(true) : void run());
  const set = (k: GroupKey) => (v: boolean) => setOn((o) => ({ ...o, [k]: v }));
  const nothing = (Object.keys(items) as GroupKey[]).every((k) => items[k].count === 0) && est.images.unused.length === 0 && est.volumes.all.length === 0;

  const imageSeg = [{ value: 'dangling', label: t('res.cleanup.images.dangling') }, { value: 'unused', label: t('res.cleanup.images.unused') }];
  const volumeSeg = [{ value: 'anonymous', label: t('res.cleanup.volumes.anonymous') }, { value: 'all', label: t('res.cleanup.volumes.all') }];
  const names = (list: string[]) => <ul className="dk-cl-names">{list.slice(0, 8).map((n) => <li key={n}>{n}</li>)}{list.length > 8 && <li>{t('res.cleanup.more', { n: list.length - 8 })}</li>}</ul>;

  return (
    <>
      {header}
      <DiskBar showCleanup={false} />
      {result && (
        <section className="dk-card dk-result" aria-live="polite">
          <div className="dk-card-h">
            <h3><Icon name="check" /> {t('res.cleanup.freed', { size: formatBytes(result.reduce((a, o) => a + o.bytes, 0)) })}</h3>
            <Button size="sm" variant="ghost" icon="close" onClick={() => setResult(null)}>{t('common.close')}</Button>
          </div>
          <ul className="dk-cl-names">
            {result.map((o) => (
              <li key={o.key} className={o.error ? 'dk-fail' : undefined}>
                {t(`res.cleanup.out.${o.key}`, { n: o.count })}{o.bytes > 0 && `, ${formatBytes(o.bytes)}`}{o.error && `: ${o.error}`}
              </li>
            ))}
          </ul>
        </section>
      )}
      {nothing ? (
        <EmptyState icon="check" hue="log" title={t('res.cleanup.clean.title')} text={t('res.cleanup.clean.text')} />
      ) : (
        <section className="dk-card">
          <CleanRow
            k="images" item={items.images} checked={on.images} onChange={set("images")} sizesReady={sizesReady} icon="image" hue="sw"
            title={t('res.cleanup.images.title')}
            empty={imageMode === 'dangling' && est.images.unused.length > 0 ? tn('res.cleanup.images.otherMode', { n: est.images.unused.length }) : undefined}
            text={imageMode === 'dangling' ? tn('res.cleanup.images.danglingText', { n: items.images.count }) : tn('res.cleanup.images.unusedText', { n: items.images.count })}
            extra={<Segmented options={imageSeg} value={imageMode} onChange={(v) => setImageMode(v as ImageMode)} aria-label={t('res.cleanup.images.title')} />}
            detail={names(imgList.map((i) => realTags(i.RepoTags)[0] ?? `<none> ${shortId(i.Id)}`))}
          />
          <CleanRow
            k="containers" item={items.containers} checked={on.containers} onChange={set("containers")} sizesReady={sizesReady} icon="box" hue="file"
            title={t('res.cleanup.containers.title')}
            text={tn('res.cleanup.containers.text', { n: items.containers.count })}
            detail={names(est.containers.list.map(containerName))}
          />
          <CleanRow
            k="volumes" item={items.volumes} checked={on.volumes} onChange={set("volumes")} sizesReady={sizesReady} icon="database" hue="term" warn
            title={t('res.cleanup.volumes.title')}
            empty={volumeMode === 'anonymous' && est.volumes.all.length > 0 ? tn('res.cleanup.volumes.otherMode', { n: est.volumes.all.length }) : undefined}
            text={volumeMode === 'anonymous' ? tn('res.cleanup.volumes.anonText', { n: items.volumes.count }) : tn('res.cleanup.volumes.allText', { n: items.volumes.count })}
            extra={
              <>
                <Segmented options={volumeSeg} value={volumeMode} onChange={(v) => setVolumeMode(v as VolumeMode)} aria-label={t('res.cleanup.volumes.title')} />
                <p className="dk-warn-p"><Icon name="alert" />{t('res.cleanup.volumes.warn')}</p>
              </>
            }
            detail={names(volList.map((v) => (isAnonymousVolume(v.Name) ? `${v.Name.slice(0, 12)}… (${t('res.volumes.anonymous')})` : v.Name)))}
          />
          <CleanRow
            k="cache" item={items.cache} checked={on.cache} onChange={set("cache")} sizesReady={sizesReady} icon="layers" hue="log"
            title={t('res.cleanup.cache.title')}
            text={tn('res.cleanup.cache.text', { n: items.cache.count })}
          />
          <CleanRow
            k="networks" item={items.networks} checked={on.networks} onChange={set("networks")} sizesReady={sizesReady} icon="net" hue="svc"
            title={t('res.cleanup.networks.title')}
            text={tn('res.cleanup.networks.text', { n: items.networks.count })}
            detail={names(est.networks.list.map((n) => n.Name))}
          />
          <div className="dk-cl-foot">
            <div className="dk-cl-total">
              <b>{active.length ? (sizesReady ? t('res.cleanup.total', { size: formatBytes(total) }) : t('disk.loading')) : t('res.cleanup.pick')}</b>
              <span className="dk-muted">{t('res.cleanup.estimate')}</span>
            </div>
            <Button variant="primary" icon="broom" loading={running} disabled={active.length === 0 || running} onClick={go}>
              {t('res.cleanup.free', { size: formatBytes(total) })}
            </Button>
          </div>
          <p className="dk-note"><Icon name="info" />{t('res.cleanup.auto')} <button type="button" className="dk-link" onClick={() => navigate({ view: 'autoupdate' })}>{t('nav.autoupdate')}</button></p>
        </section>
      )}
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        onConfirm={run}
        title={t('res.cleanup.confirmTitle', { n: volList.length })}
        description={t('res.cleanup.confirmDesc')}
        confirmLabel={t('res.cleanup.confirmGo')}
        confirmText={t('res.cleanup.confirmWord')}
        icon="trash"
        danger
      />
    </>
  );
}

function CleanRow({ k, item, checked, onChange, sizesReady, icon, hue, title, text, empty, extra, detail, warn }: {
  k: GroupKey;
  item: { count: number; bytes: number };
  checked: boolean;
  onChange(v: boolean): void;
  sizesReady: boolean;
  icon: string;
  hue: string;
  title: string;
  text: string;
  /** Shown instead of "nothing to remove" when there is nothing in this mode. */
  empty?: string;
  extra?: ReactNode;
  detail?: ReactNode;
  warn?: boolean;
}) {
  return (
    <div className={`dk-cl hue-${hue}${warn ? ' dk-cl--warn' : ''}`}>
      <div className="dk-cl-top">
        <Checkbox checked={checked && item.count > 0} disabled={item.count === 0} onChange={onChange} aria-label={title} />
        <span className="dk-cl-ic"><Icon name={icon} /></span>
        <div className="dk-cl-tx">
          <b>{title}</b>
          <span className="dk-muted">{item.count === 0 ? empty ?? t('res.cleanup.nothing') : text}</span>
        </div>
        <span className="dk-cl-sz">{item.count === 0 ? '' : k === 'networks' ? tn('res.cleanup.count', { n: item.count }) : sizesReady ? formatBytes(item.bytes) : '…'}</span>
      </div>
      {extra && <div className="dk-cl-extra">{extra}</div>}
      {detail && item.count > 0 && <details className="dk-cl-det"><summary>{t('res.cleanup.show')}</summary>{detail}</details>}
    </div>
  );
}
