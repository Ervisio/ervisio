import { useEffect, useRef, type ComponentType } from 'react';
import { useParams } from 'react-router-dom';
import { EmptyState, Skeleton } from '../ui';
import { useT } from '../i18n';
import { usePlugins } from './PluginsProvider';
import type { PluginPageDef, PluginSDK } from './types';

/** Route element for /p/:plugin/:page. */
export default function PluginPage() {
  const { plugin = '', page = '' } = useParams();
  const t = useT('shell');
  const { pages, plugins, loading, errors } = usePlugins();
  const reg = pages.find((p) => p.plugin === plugin && p.id === page);
  const manifest = plugins.find((p) => p.id === plugin);

  if (!reg) {
    if (loading) return <div style={{ padding: 28 }}><Skeleton lines={4} /></div>;
    return (
      <div style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <EmptyState
          icon="plugins"
          hue="plg"
          title={errors[plugin] ? t('plugin.failed', { name: manifest?.name ?? plugin }) : t('plugin.notFound')}
          text={errors[plugin] ?? t('plugin.notFoundText')}
        />
      </div>
    );
  }
  return <Mount key={`${plugin}/${page}`} def={reg.def} sdk={reg.sdk} />;
}

/** Mounts a plugin page or widget definition (React component or framework-free render function). */
export function Mount({ def, sdk }: { def: PluginPageDef; sdk: PluginSDK }) {
  const ref = useRef<HTMLDivElement>(null);
  const isComponent = typeof def === 'function';
  useEffect(() => {
    if (isComponent || !ref.current) return;
    const cleanup = (def as { render(c: HTMLElement, s: PluginSDK): void | (() => void) }).render(ref.current, sdk);
    return () => {
      if (typeof cleanup === 'function') cleanup();
    };
  }, [def, isComponent, sdk]);
  if (isComponent) {
    const C = def as ComponentType<{ sdk: PluginSDK }>;
    return <C sdk={sdk} />;
  }
  return <div ref={ref} />;
}
