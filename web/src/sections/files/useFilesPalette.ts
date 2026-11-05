import type { TFn } from '../../i18n';
import { usePaletteActions } from '../index';

interface PaletteArgs {
  t: TFn;
  home: string;
  goPlace(loc: string): void;
  newFolder(): void;
  toggleSplit(): void;
}

export function useFilesPalette({ t, home, goPlace, newFolder, toggleSplit }: PaletteArgs) {
  usePaletteActions('files', [
    {
      id: 'files.home',
      title: t('palette.home'),
      hint: t('title'),
      icon: 'home',
      hue: 'file',
      run: () => goPlace(home),
    },
    {
      id: 'files.trash',
      title: t('palette.trash'),
      hint: t('title'),
      icon: 'trash',
      hue: 'file',
      run: () => goPlace('trash:'),
    },
    {
      id: 'files.newfolder',
      title: t('palette.newFolder'),
      hint: t('title'),
      icon: 'files',
      hue: 'file',
      run: newFolder,
    },
    {
      id: 'files.split',
      title: t('palette.split'),
      hint: t('title'),
      icon: 'columns',
      hue: 'file',
      run: toggleSplit,
    },
  ]);
}
