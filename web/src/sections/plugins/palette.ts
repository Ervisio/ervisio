import { useNavigate } from 'react-router-dom';
import { useT } from '../../i18n';
import type { PaletteAction } from '../registry';

export default function usePalette(): PaletteAction[] {
  const t = useT('plugins');
  const go = useNavigate();
  return [
    { id: 'plugins.browse', title: t('palette.browse'), hint: t('title'), icon: 'plugins', hue: 'plg', keywords: ['install', 'store', 'extension'], run: () => go('/plugins?tab=browse') },
    { id: 'plugins.security', title: t('palette.security'), hint: t('title'), icon: 'shield', hue: 'plg', keywords: ['permissions', 'signature', 'root'], run: () => go('/plugins?tab=security') },
    { id: 'plugins.updates', title: t('palette.updates'), hint: t('title'), icon: 'refresh', hue: 'plg', run: () => go('/plugins?tab=updates') },
  ];
}
