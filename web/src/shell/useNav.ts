import { useMemo } from 'react';
import { useT } from '../i18n';
import { usePlugins } from '../plugins';
import { SECTIONS, useRailBadges, useSectionBadgeHooks, type RailBadge } from '../sections';
import type { IconName } from '../ui';

export interface NavEntry {
  key: string;
  to: string;
  icon: IconName;
  /** A plugin's logo URL, drawn instead of the icon. */
  logo?: string;
  label: string;
  /** class that sets --h / --s */
  hueClass: string;
  badge?: RailBadge;
  end?: boolean;
  kind: 'section' | 'plugin' | 'settings';
}

const HUES = new Set(['ov', 'term', 'file', 'log', 'svc', 'sw', 'usr', 'plg']);

export function useNav() {
  const t = useT('shell');
  const { railPages } = usePlugins();
  const registered = useRailBadges();
  const fromHooks = useSectionBadgeHooks();
  return useMemo(() => {
    const entry = (s: (typeof SECTIONS)[number]): NavEntry => ({
      key: s.id,
      to: s.path,
      icon: s.icon,
      label: t(s.titleKey),
      hueClass: `hue-${s.hue}`,
      badge: registered[s.id] ?? fromHooks[s.id],
      end: s.path === '/',
      kind: s.id === 'settings' ? 'settings' : 'section',
    });
    const main = SECTIONS.filter((s) => s.rail).map(entry);
    const settings = entry(SECTIONS.find((s) => s.id === 'settings')!);
    const plugins: NavEntry[] = railPages.map((p) => ({
      key: `p:${p.plugin}/${p.page}`,
      to: `/p/${p.plugin}/${p.page}`,
      icon: p.icon,
      logo: p.logo,
      label: p.title,
      hueClass: `hue-${p.color && HUES.has(p.color) ? p.color : 'plg'}`,
      kind: 'plugin',
    }));
    return { main, settings, plugins };
  }, [t, railPages, registered, fromHooks]);
}
