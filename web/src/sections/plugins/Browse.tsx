import { useMemo, useState } from 'react';
import { useT } from '../../i18n';
import { Button, Chip, EmptyState, Icon } from '../../ui';
import { H4, hueOf, PlatformIcons, Tile, TrustBadge } from './Parts';
import type { CatalogEntry, CatalogView } from './types';

interface Props {
  catalog: CatalogView | null;
  query: string;
  busyId: string;
  onInstall(e: CatalogEntry): void;
}

const fmtInstalls = (n: number) => (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));

export function Browse({ catalog, query, busyId, onInstall }: Props) {
  const t = useT('plugins');
  const [cat, setCat] = useState('');
  const all = useMemo(() => catalog?.plugins ?? [], [catalog]);
  const featured = useMemo(() => all.find((e) => e.featured && !e.installed) ?? all.filter((e) => !e.installed && e.verified).sort((a, b) => b.installs - a.installs)[0], [all]);
  const q = query.trim().toLowerCase();
  const list = useMemo(
    () =>
      all
        .filter((e) => (!cat || e.category === cat) && (!q || `${e.name} ${e.description} ${e.category} ${e.author}`.toLowerCase().includes(q)))
        .sort((a, b) => b.installs - a.installs),
    [all, cat, q],
  );

  if (!catalog) return <div className="plugins-pad"><EmptyState icon="plugins" hue="plg" title={t('loading')} /></div>;
  return (
    <div className="plugins-browse">
      {catalog.warning && <div className="plugins-note is-warn">{catalog.warning}</div>}
      {featured && !q && !cat && (
        <div className={`plugins-feat hue-${hueOf(featured.color)}`}>
          <Tile icon={featured.icon} logo={featured.logo} color={featured.color} size="lg" />
          <div>
            <h2>{featured.name}</h2>
            <p>{featured.notes || featured.description}</p>
          </div>
          <PlatformIcons platforms={featured.platforms} />
          <Button size="lg" disabled={!!featured.incompatible} title={featured.incompatible} loading={busyId === featured.id} onClick={() => onInstall(featured)}>{t('install')}</Button>
        </div>
      )}
      <div className="plugins-cats" role="group" aria-label={t('categories')}>
        <Chip pressed={!cat} onClick={() => setCat('')}>{t('allCategories')}</Chip>
        {catalog.categories.map((c) => (
          <Chip key={c.id} pressed={cat === c.id} icon={c.icon} hue={hueOf(c.color)} onClick={() => setCat(cat === c.id ? '' : c.id)}>{c.name}</Chip>
        ))}
      </div>
      <H4>{t('popular')}</H4>
      {list.length === 0 ? (
        <EmptyState icon="search" hue="plg" title={t('noMatch')} text={t('noMatchText')} />
      ) : (
        <div className="plugins-bgrid">
          {list.map((e) => (
            <div key={e.id} className="plugins-bc">
              <Tile icon={e.icon} logo={e.logo} color={e.color} />
              <div className="plugins-bc-tx">
                <b>{e.name}</b>
                <small>{e.description}</small>
                <div className="plugins-bc-m">
                  <TrustBadge p={{ verified: e.verified }} />
                  {e.installs > 0 && <span className="plugins-muted">{t('installs', { count: fmtInstalls(e.installs) })}</span>}
                </div>
              </div>
              <PlatformIcons platforms={e.platforms} />
              {e.installed ? (
                <span className="plugins-installed"><Icon name="check" size={14} /> {t('installed')}</span>
              ) : e.incompatible ? (
                <span className="plugins-muted" title={e.incompatible}>{t('incompatible')}</span>
              ) : (
                <Button variant="primary" size="sm" loading={busyId === e.id} onClick={() => onInstall(e)}>{t('install')}</Button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
