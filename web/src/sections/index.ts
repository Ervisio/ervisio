import { lazy, type ComponentType, type LazyExoticComponent } from 'react';
import type { Platform } from '../api/types';
import type { IconName, HueId } from '../ui';

export interface SectionDef {
  id: string;
  /** Route path ("/" for overview). */
  path: string;
  icon: IconName;
  /** CSS variable of the section colour, e.g. var(--h-svc). */
  colorVar: string;
  /** Section hue id for ui components (`hue` props). */
  hue: HueId | 'set';
  /** Key in the `shell` namespace, e.g. nav.services. */
  titleKey: string;
  page: LazyExoticComponent<ComponentType>;
  /** Optional native ErvisiOS view. Classic keeps the normal section page. */
  desktopPage?: LazyExoticComponent<ComponentType>;
  desktopTitleKey?: string;
  /** Shown in the rail / dock. Settings is placed separately by the shell. */
  rail: boolean;
  /** Available as a separate page in Classic, without duplicating a desktop app. */
  desktopHidden?: boolean;
  /** Operating systems the section works on; all when unset. */
  platforms?: Platform[];
}

export const SECTIONS: SectionDef[] = [
  {
    id: 'overview',
    path: '/',
    icon: 'overview',
    colorVar: 'var(--h-ov)',
    hue: 'ov',
    titleKey: 'nav.overview',
    rail: true,
    page: lazy(() => import('./overview')),
    desktopPage: lazy(() => import('./taskmanager')),
    desktopTitleKey: 'desktop.taskManager',
  },
  {
    id: 'taskmanager',
    path: '/taskmanager',
    icon: 'overview',
    colorVar: 'var(--h-ov)',
    hue: 'ov',
    titleKey: 'nav.taskManager',
    rail: true,
    desktopHidden: true,
    page: lazy(() => import('./taskmanager')),
  },
  {
    id: 'terminal',
    path: '/terminal',
    icon: 'terminal',
    colorVar: 'var(--h-term)',
    hue: 'term',
    titleKey: 'nav.terminal',
    rail: true,
    page: lazy(() => import('./terminal')),
  },
  {
    id: 'files',
    path: '/files',
    icon: 'files',
    colorVar: 'var(--h-file)',
    hue: 'file',
    titleKey: 'nav.files',
    rail: true,
    page: lazy(() => import('./files')),
  },
  {
    id: 'notepad',
    path: '/notepad',
    icon: 'file',
    colorVar: 'var(--h-file)',
    hue: 'file',
    titleKey: 'nav.notepad',
    rail: false,
    page: lazy(() => import('./editor/notepad')),
  },
  {
    id: 'editor',
    path: '/editor',
    icon: 'code',
    colorVar: 'var(--h-file)',
    hue: 'file',
    titleKey: 'nav.editor',
    rail: false,
    page: lazy(() => import('./editor')),
  },
  {
    id: 'media',
    path: '/media',
    icon: 'image',
    colorVar: 'var(--h-file)',
    hue: 'file',
    titleKey: 'nav.media',
    rail: false,
    page: lazy(() => import('./media')),
  },
  {
    id: 'logs',
    path: '/logs',
    icon: 'logs',
    colorVar: 'var(--h-log)',
    hue: 'log',
    titleKey: 'nav.logs',
    rail: true,
    page: lazy(() => import('./logs')),
  },
  {
    id: 'services',
    path: '/services',
    icon: 'services',
    colorVar: 'var(--h-svc)',
    hue: 'svc',
    titleKey: 'nav.services',
    rail: true,
    page: lazy(() => import('./services')),
  },
  {
    id: 'software',
    path: '/software',
    icon: 'software',
    colorVar: 'var(--h-sw)',
    hue: 'sw',
    titleKey: 'nav.software',
    rail: true,
    page: lazy(() => import('./software')),
  },
  {
    id: 'users',
    path: '/users',
    icon: 'users',
    colorVar: 'var(--h-usr)',
    hue: 'usr',
    titleKey: 'nav.users',
    rail: true,
    page: lazy(() => import('./users')),
  },
  {
    id: 'plugins',
    path: '/plugins',
    icon: 'plugins',
    colorVar: 'var(--h-plg)',
    hue: 'plg',
    titleKey: 'nav.plugins',
    rail: true,
    page: lazy(() => import('./plugins')),
  },
  {
    id: 'settings',
    path: '/settings',
    icon: 'cog',
    colorVar: 'var(--ink)',
    hue: 'set',
    titleKey: 'nav.settings',
    rail: false,
    page: lazy(() => import('./settings')),
  },
];

export const sectionById = (id: string) => SECTIONS.find((s) => s.id === id);
export {
  registerPaletteActions,
  usePaletteActions,
  useAllPaletteActions,
  setRailBadge,
  useRailBadge,
  useRailBadges,
  useSectionBadgeHooks,
  useSectionBackgroundHooks,
} from './registry';
export type { PaletteAction, RailBadge } from './registry';
export { useFocusMode, setFocusMode } from '../shell/focus';
export { notify, dismissNotice, type Notice, type NoticeInput } from '../shell/notifications';
