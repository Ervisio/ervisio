import { Fragment, useMemo, useState } from 'react';
import { resetEngine, docker, errorText } from '../api/engine';
import { containerName, formatBytes, relativeTime, shortId } from '../api/format';
import { containers, diskUsage, images } from '../api/resources';
import type { ImageSummary } from '../api/types';
import { t, tn } from '../i18n';
import { Badge, Button, Checkbox, ConfirmDialog, EmptyState, Icon, IconButton, Input, Segmented, Skeleton, toast } from '../kit';
import { navigate, useSearch } from '../router';
import { DiskBar } from '../ui/DiskBar';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { groupContainers, matchesText, UsedBy } from './resources/bits';
import { realTags, refPath } from './resources/imageRef';
import { LayersRow } from './resources/LayersRow';
import { PullPanel } from './resources/PullPanel';
import { checkAll, forget, recheck, useUpdates } from './resources/updates';

type Filter = 'all' | 'used' | 'unused' | 'updates';

interface Row {
  key: string;
  img: ImageSummary;
  /** "repo:tag", or undefined for an untagged (dangling) image. */
  ref?: string;
  repo: string;
  tag: string;
  tagCount: number;
}

/** One row per repo:tag, like the design; an image without tags gets one <none> row. */
function toRows(list: ImageSummary[]): Row[] {
  const rows: Row[] = [];
  for (const img of list) {
    const tags = realTags(img.RepoTags);
    if (!tags.length) rows.push({ key: img.Id, img, repo: '<none>', tag: '<none>', tagCount: 0 });
    for (const ref of tags) {
      const i = ref.lastIndexOf(':');
      rows.push({ key: `${img.Id}|${ref}`, img, ref, repo: i > ref.lastIndexOf('/') ? ref.slice(0, i) : ref, tag: i > ref.lastIndexOf('/') ? ref.slice(i + 1) : 'latest', tagCount: tags.length });
    }
  }
  return rows.sort((a, b) => b.img.Created - a.img.Created || a.repo.localeCompare(b.repo));
}

export function ImagesPage() {
  const { data, error, loading } = images.use();
  const cts = containers.use().data ?? [];
  const df = diskUsage.use().data;
  const global = useSearch();
  const upd = useUpdates();
  const [filter, setFilter] = useState<Filter>('all');
  const [q, setQ] = useState('');
  const [pull, setPull] = useState<{ ref: string; auto: boolean; n: number } | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const [removing, setRemoving] = useState<Row | null>(null);
  const [force, setForce] = useState(false);

  const usedBy = useMemo(() => groupContainers(cts, (c) => [c.ImageID]), [cts]);
  const rows = useMemo(() => toRows(data ?? []), [data]);
  const newer = (r: Row) => !!r.ref && upd.get(r.ref)?.kind === 'newer';
  const counts = {
    all: rows.length,
    used: rows.filter((r) => usedBy.has(r.img.Id)).length,
    unused: rows.filter((r) => !usedBy.has(r.img.Id)).length,
    updates: rows.filter(newer).length,
  };
  const visible = rows.filter((r) => {
    if (filter === 'used' && !usedBy.has(r.img.Id)) return false;
    if (filter === 'unused' && usedBy.has(r.img.Id)) return false;
    if (filter === 'updates' && !newer(r)) return false;
    return matchesText(q, r.repo, r.tag, r.img.Id) && matchesText(global, r.repo, r.tag, r.img.Id);
  });

  const totalSize = df?.LayersSize ?? (data ?? []).reduce((a, i) => a + i.Size, 0);
  const subtitle = data
    ? [tn('res.images.sub', { n: new Set(rows.map((r) => r.img.Id)).size, size: formatBytes(totalSize) }), counts.updates > 0 && t('res.images.subNewer', { n: counts.updates })].filter(Boolean).join('. ')
    : '';

  const header = (
    <PageHeader
      icon="image"
      hue="sw"
      title={t('nav.images')}
      subtitle={subtitle}
      actions={
        <>
          <Button icon="refresh" loading={upd.checking > 0} onClick={() => void checkAll(data ?? [], true)}>{t('res.images.check')}</Button>
          <Button icon="broom" onClick={() => navigate({ view: 'cleanup' })}>{t('disk.cleanup')}</Button>
          <Button variant="primary" icon="download" onClick={() => setPull({ ref: '', auto: false, n: Date.now() })}>{t('res.images.pull')}</Button>
        </>
      }
    />
  );

  if (!data && error) return <>{header}<ErrorState error={error} onRetry={() => { resetEngine(); void images.refresh(); }} /></>;
  if (!data && loading) return <>{header}<Skeleton height={90} style={{ borderRadius: 18 }} /><Skeleton lines={6} /></>;

  const doRemove = async () => {
    const r = removing;
    if (!r) return;
    // An image with several tags is only untagged by its name; the last tag (or an untagged image) goes by id.
    const target = r.ref && r.tagCount > 1 && !force ? r.ref : r.img.Id;
    try {
      await docker.delete(`/images/${refPath(target)}`, { force: force ? '1' : '0' });
      toast.ok(t('res.images.removed', { name: r.ref ?? shortId(r.img.Id) }));
      if (r.ref) forget(r.ref);
      void images.refresh();
      void diskUsage.refresh();
    } catch (e) {
      toast.err(t('res.images.removeFail', { name: r.ref ?? shortId(r.img.Id) }), errorText(e));
    } finally {
      setRemoving(null);
      setForce(false);
    }
  };

  const used = removing ? usedBy.get(removing.img.Id) ?? [] : [];
  const segs = (['all', 'used', 'unused', 'updates'] as Filter[]).map((f) => ({ value: f, label: `${t(`res.images.f.${f}`)} ${counts[f]}` }));

  return (
    <>
      {header}
      <DiskBar />
      {pull && (
        <PullPanel
          key={pull.n}
          initial={pull.ref}
          autostart={pull.auto}
          onClose={() => setPull(null)}
          onDone={(ref) => {
            void images.refresh();
            void diskUsage.refresh();
            forget(ref);
          }}
        />
      )}
      <section className="dk-card">
        <div className="dk-bar1">
          <Input fieldClassName="dk-grow" icon="search" placeholder={t('res.images.filter')} aria-label={t('res.images.filter')} value={q} onChange={(e) => setQ(e.target.value)} />
          <Segmented options={segs} value={filter} onChange={(v) => setFilter(v as Filter)} aria-label={t('nav.images')} />
        </div>
        {upd.lastAt > 0 && <small className="dk-muted">{t('res.images.checked', { when: relativeTime(upd.lastAt / 1000) })}</small>}
        {rows.length === 0 ? (
          <EmptyState icon="image" hue="sw" title={t('res.images.empty.title')} text={t('res.images.empty.text')} action={<Button variant="primary" icon="download" onClick={() => setPull({ ref: '', auto: false, n: Date.now() })}>{t('res.images.pull')}</Button>} />
        ) : visible.length === 0 ? (
          <EmptyState icon="search" hue="sw" title={t('res.noMatch.title')} text={t('res.noMatch.text')} />
        ) : (
          <div className="dk-tablewrap">
            <table className="dk-table">
              <thead>
                <tr>
                  <th>{t('res.images.col.image')}</th>
                  <th>{t('res.images.col.tag')}</th>
                  <th>{t('res.col.usedBy')}</th>
                  <th />
                  <th className="dk-hide-md">{t('res.col.created')}</th>
                  <th className="dk-num">{t('res.col.size')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {visible.map((r) => {
                  const u = r.ref ? upd.get(r.ref) : undefined;
                  const isOpen = open === r.key;
                  const name = r.ref ?? shortId(r.img.Id);
                  return (
                    <Fragment key={r.key}>
                      <tr className={isOpen ? 'dk-sel' : undefined}>
                        <td>
                          <span className={`dk-repo${r.ref ? '' : ' dk-repo--none'}`} title={r.repo}>{r.repo}</span>
                          {!r.ref && <small className="dk-muted dk-id">{shortId(r.img.Id)}</small>}
                        </td>
                        <td><span className="dk-tag">{r.tag}</span></td>
                        <td><UsedBy list={usedBy.get(r.img.Id) ?? []} /></td>
                        <td>
                          {!r.ref && <Badge tone="neutral">{t('res.images.dangling')}</Badge>}
                          {u?.kind === 'newer' && <span className="dk-upd" title={t('res.images.newerHint')}>{t('res.images.newer')}</span>}
                          {u?.kind === 'current' && <span className="dk-muted">{t('res.images.current')}</span>}
                          {u?.kind === 'local' && <span className="dk-muted" title={t('res.images.localHint')}>{t('res.images.local')}</span>}
                          {u?.kind === 'error' && <span className="dk-muted" title={u.message}>{t('res.images.checkFail')}</span>}
                        </td>
                        <td className="dk-hide-md dk-muted">{relativeTime(r.img.Created)}</td>
                        <td className="dk-num">{formatBytes(r.img.Size)}</td>
                        <td>
                          <div className="dk-act">
                            {r.ref && <IconButton icon="download" size="sm" variant="ghost" label={t('res.images.pullAgain')} onClick={() => setPull({ ref: r.ref!, auto: true, n: Date.now() })} />}
                            {r.ref && <IconButton icon="play" size="sm" variant="ghost" label={t('res.images.run')} onClick={() => navigate({ view: 'create', image: r.ref })} />}
                            <IconButton icon="layers" size="sm" variant="ghost" label={t('res.images.layers')} onClick={() => setOpen(isOpen ? null : r.key)} />
                            <IconButton icon="trash" size="sm" variant="ghost" label={t('common.remove')} onClick={() => { setRemoving(r); setForce(false); }} />
                          </div>
                        </td>
                      </tr>
                      {isOpen && (
                        <tr className="dk-exp">
                          <td colSpan={7}>
                            <div className="dk-exp-in">
                              <div className="dk-exp-h">
                                <b>{t('res.images.layersOf', { name })}</b>
                                <code className="dk-muted">{r.img.Id.replace('sha256:', '').slice(0, 24)}</code>
                                {r.ref && u?.kind !== 'newer' && (
                                  <Button size="sm" variant="ghost" icon="refresh" onClick={() => void recheck(r.ref!, r.img)}>{t('res.images.checkOne')}</Button>
                                )}
                                <IconButton icon="close" size="sm" variant="ghost" label={t('common.close')} onClick={() => setOpen(null)} />
                              </div>
                              <LayersRow id={r.img.Id} />
                            </div>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <ConfirmDialog
        open={!!removing}
        onClose={() => { setRemoving(null); setForce(false); }}
        onConfirm={doRemove}
        title={t('res.images.removeTitle', { name: removing?.ref ?? shortId(removing?.img.Id ?? '') })}
        description={removing && removing.ref && removing.tagCount > 1 && !force ? t('res.images.untagDesc') : t('res.images.removeDesc')}
        confirmLabel={t('common.remove')}
        icon="trash"
        danger
      >
        {used.length > 0 && (
          <>
            <p className="dk-warn-p"><Icon name="alert" />{t('res.images.inUse', { names: used.map(containerName).join(', ') })}</p>
            <Checkbox checked={force} onChange={setForce} label={t('res.images.force')} />
          </>
        )}
      </ConfirmDialog>
    </>
  );
}
