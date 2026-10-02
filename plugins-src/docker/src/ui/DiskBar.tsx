import { diskUsage } from '../api/resources';
import { formatBytes } from '../api/format';
import { summarizeDf } from '../api/system';
import { t } from '../i18n';
import { Button, Skeleton } from '../kit';
import { navigate } from '../router';

/**
 * The shared "Disk used by Docker" card (design 031 a): one bar split into images, containers, volumes and build
 * cache, the part that can be freed drawn lighter, with a legend. Used on Images, Volumes and Cleanup.
 */
export function DiskBar({ showCleanup = true }: { showCleanup?: boolean }) {
  const { data, error } = diskUsage.use();
  const parts = data ? summarizeDf(data) : [];
  const total = parts.reduce((a, p) => a + p.total, 0);
  const free = parts.reduce((a, p) => a + p.reclaimable, 0);
  return (
    <section className="dk-card dk-disk">
      <div className="dk-card-h">
        <h3>{t('disk.title')}</h3>
        {data && (
          <span className="dk-muted">
            {t('disk.total', { size: formatBytes(total) })}
            {free > 0 && `, ${t('disk.reclaimable', { size: formatBytes(free) })}`}
          </span>
        )}
        {showCleanup && data && free > 0 && (
          <Button size="sm" icon="broom" onClick={() => navigate({ view: 'cleanup' })}>{t('disk.cleanup')}</Button>
        )}
      </div>
      {!data && !error && <><Skeleton height={14} /><small className="dk-muted">{t('disk.loading')}</small></>}
      {!data && error && <small className="dk-muted">{error.message}</small>}
      {data && (
        <>
          <div className="dk-sbar" role="img" aria-label={t('disk.title')}>
            {parts.map((p) => (
              <span key={p.key} className={`hue-${p.hue} dk-sbar-part`} style={{ flex: Math.max(p.total, total * 0.004) }}>
                <i style={{ flex: Math.max(0, p.total - p.reclaimable) }} />
                <i className="dk-sbar-free" style={{ flex: p.reclaimable }} />
              </span>
            ))}
          </div>
          <div className="dk-legend">
            {parts.map((p) => (
              <div key={p.key} className={`hue-${p.hue}`}>
                <span className="dk-sw" />
                <b>{t(`disk.${p.key}`)}</b> {formatBytes(p.total)}
                {p.reclaimable > 0 && <span className="dk-muted">({t('disk.reclaimable', { size: formatBytes(p.reclaimable) })})</span>}
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
