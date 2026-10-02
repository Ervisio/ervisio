import { containers, images, info, networks, volumes } from '../api/resources';
import { resetEngine } from '../api/engine';
import { groupByStack } from '../api/model';
import { t } from '../i18n';
import { Icon, IconButton, Input } from '../kit';
import { navigate, sectionOf, setSearch, useRoute, useSearch, type NavId } from '../router';
import { ErrorState } from '../ui/ErrorState';
import { NAV_ICONS } from '../ui/icons';
import { ViewHost } from '../views/registry';

const GROUPS: { label: string; items: NavId[] }[] = [
  { label: 'nav.workloads', items: ['containers', 'stacks', 'templates'] },
  { label: 'nav.resources', items: ['images', 'volumes', 'networks', 'registries'] },
  { label: 'nav.tools', items: ['cleanup', 'autoupdate', 'alerts'] },
];

/** Views that list things the search box can filter. */
const SEARCHABLE: NavId[] = ['containers', 'stacks', 'images'];
/** Views that work without a running engine. */
const OFFLINE_OK: NavId[] = ['registries'];

/** The page: inner sidebar, search box, and the view for the current route. */
export function App() {
  const route = useRoute();
  const section = sectionOf(route);
  const q = useSearch();
  const cs = containers.use();
  const imgs = images.use();
  const vols = volumes.use();
  const nets = networks.use();
  const eng = info.use();

  const counts: Partial<Record<NavId, number>> = {};
  if (cs.data) {
    counts.containers = cs.data.length;
    counts.stacks = groupByStack(cs.data).filter((g) => g.name).length;
  }
  if (imgs.data) counts.images = imgs.data.length;
  if (vols.data) counts.volumes = vols.data.length;
  if (nets.data) counts.networks = nets.data.length;

  const onSearch = (v: string) => {
    setSearch(v);
    if (v && !SEARCHABLE.includes(section)) navigate({ view: 'containers' }, { root: true });
  };

  const blocked = !cs.data && cs.error && !OFFLINE_OK.includes(section);

  return (
    <div className="dk-root hue-file">
      <div className="dk-shell">
        <nav className="dk-nav" aria-label={t('nav.aria')}>
          {GROUPS.map((g) => (
            <div key={g.label} style={{ display: 'contents' }}>
              <div className="dk-nav-gl">{t(g.label)}</div>
              {g.items.map((id) => (
                <button key={id} type="button" className="dk-nav-it" aria-current={section === id ? 'page' : undefined} onClick={() => navigate({ view: id } as never, { root: true })}>
                  <Icon name={NAV_ICONS[id]} />
                  {t(`nav.${id}`)}
                  {counts[id] !== undefined && <b>{counts[id]}</b>}
                </button>
              ))}
            </div>
          ))}
          {eng.data && <div className="dk-nav-ft"><span className="dk-dot dk-dot--ok" />{t('shell.engine', { version: eng.data.version.Version })}</div>}
        </nav>
        <div className="dk-main">
          <div className="dk-top">
            <Input
              fieldClassName="dk-search"
              icon="search"
              value={q}
              placeholder={t('shell.search')}
              aria-label={t('shell.search')}
              onChange={(e) => onSearch(e.target.value)}
              end={q ? <IconButton icon="close" label={t('common.close')} size="sm" onClick={() => setSearch('')} /> : undefined}
            />
          </div>
          <div className="dk-view">
            {blocked ? <ErrorState error={cs.error!} onRetry={() => { resetEngine(); void containers.refresh(); }} /> : <ViewHost key={JSON.stringify(route)} route={route} />}
          </div>
        </div>
      </div>
    </div>
  );
}
