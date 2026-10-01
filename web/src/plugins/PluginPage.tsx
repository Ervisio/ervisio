import { useParams } from 'react-router-dom';
import { EmptyState, Skeleton } from '../ui';
import { useT } from '../i18n';
import { PluginFrame } from './PluginFrame';
import { usePlugins } from './PluginsProvider';

/** Route element for /p/:plugin/:page: the page runs in the plugin's sandboxed frame, inside the shell. */
export default function PluginPage() {
  const { plugin = '', page = '' } = useParams();
  const t = useT('shell');
  const { pages, plugins, loading } = usePlugins();
  const reg = pages.find((p) => p.plugin === plugin && p.id === page);
  const manifest = plugins.find((p) => p.id === plugin);

  if (!reg || !manifest) {
    if (loading) return <div style={{ padding: 28 }}><Skeleton lines={4} /></div>;
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <EmptyState icon="plugins" hue="plg" title={t('plugin.notFound')} text={t('plugin.notFoundText')} />
      </div>
    );
  }
  return <PluginFrame plugin={manifest} view={{ kind: 'page', id: reg.id }} title={`${manifest.name}: ${reg.title}`} />;
}
