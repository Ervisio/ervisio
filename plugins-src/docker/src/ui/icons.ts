import { getSdk } from '../sdk';

/** Icons the app kit does not have. Use them by name (`<Icon name="box" />`) once activate() has run. */
export function registerDockerIcons(): void {
  const reg = getSdk().ui.registerIcon as ((name: string, svg: string) => void) | undefined;
  if (!reg) return;
  reg('box', '<path d="M21 8l-9-5-9 5v8l9 5 9-5z"/><path d="M3 8l9 5 9-5M12 13v8"/>');
  reg('layers', '<path d="M12 3l8 4.5-8 4.5-8-4.5z"/><path d="M4 12l8 4.5 8-4.5M4 16.5L12 21l8-4.5"/>');
  reg('store', '<path d="M4 9l1.5-5h13L20 9"/><path d="M4 9v10a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1V9"/><path d="M4 9h16M9.5 13h5"/>');
  reg('database', '<ellipse cx="12" cy="5.5" rx="7.5" ry="2.8"/><path d="M4.5 5.5v13c0 1.5 3.4 2.8 7.5 2.8s7.5-1.3 7.5-2.8v-13M4.5 12c0 1.5 3.4 2.8 7.5 2.8s7.5-1.3 7.5-2.8"/>');
}

/** Icon of each sidebar entry. */
export const NAV_ICONS = {
  containers: 'box',
  stacks: 'layers',
  templates: 'store',
  images: 'image',
  volumes: 'database',
  networks: 'net',
  registries: 'key',
  cleanup: 'broom',
  autoupdate: 'refresh',
  alerts: 'bell',
} as const;
