import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT } from '../../i18n';
import type { PaletteAction } from '../registry';

export default function usePalette(): PaletteAction[] {
  const t = useT('software');
  const nav = useNavigate();
  return useMemo(
    () => [
      { id: 'software.updates', title: t('palette.updates'), hint: t('title'), icon: 'software', hue: 'sw', keywords: ['upgrade', 'update', 'check'], run: () => nav('/software') },
      { id: 'software.store', title: t('palette.store'), hint: t('title'), icon: 'search', hue: 'sw', keywords: ['install', 'app', 'store', 'package'], run: () => nav('/software?tab=store') },
      { id: 'software.installed', title: t('palette.installed'), hint: t('title'), icon: 'list', hue: 'sw', keywords: ['remove', 'uninstall', 'orphans'], run: () => nav('/software?tab=installed') },
      { id: 'software.history', title: t('palette.history'), hint: t('title'), icon: 'clock', hue: 'sw', keywords: ['log', 'pacman'], run: () => nav('/software?tab=history') },
    ],
    [t, nav],
  );
}
