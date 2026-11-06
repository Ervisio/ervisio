import type { IconName } from '../../ui';
import { DRAG_TYPE, hasExternalFiles } from './dropfiles';
import type { FEntry, SortKey, SortState } from './types';

export const VIRTUAL_ICON: Record<string, IconName> = {
  'trash:': 'trash',
  'recent:': 'clock',
  'starred:': 'star',
};

export const isHidden = (e: FEntry) => e.name.startsWith('.');
export const hasFiles = hasExternalFiles;
export const hasInternal = (dt: DataTransfer) => Array.from(dt.types).includes(DRAG_TYPE);
export const nextSort = (sort: SortState, key: SortKey): SortState => ({
  key,
  dir: sort.key === key && sort.dir === 'asc' ? 'desc' : 'asc',
});
