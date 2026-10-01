export const HUE_KEYS = ['ov', 'term', 'file', 'log', 'svc', 'sw', 'usr', 'plg'] as const;
export type HueKey = (typeof HUE_KEYS)[number];
export type ColourMode = 'sections' | 'distro' | 'mono';

export interface ThemeDef {
  id: string;
  name: string;
  kind: 'dark' | 'light';
  note?: string;
  /** Own colours: ignores the colour mode. */
  own?: boolean;
  custom?: boolean;
  bg: string;
  surface: string;
  sunk: string;
  line: string;
  ink: string;
  ink2: string;
  ink3: string;
  hues: Record<HueKey, string>;
  distro: string;
}

const T = (
  id: string,
  name: string,
  kind: 'dark' | 'light',
  c: [string, string, string, string, string, string, string],
  hues: string[],
  distro: string,
  note?: string,
  own?: boolean,
): ThemeDef => ({
  id,
  name,
  kind,
  note,
  own,
  bg: c[0],
  surface: c[1],
  sunk: c[2],
  line: c[3],
  ink: c[4],
  ink2: c[5],
  ink3: c[6],
  hues: Object.fromEntries(HUE_KEYS.map((k, i) => [k, hues[i]])) as Record<HueKey, string>,
  distro,
});

export const BUILTIN_THEMES: ThemeDef[] = [
  T('oled', 'OLED', 'dark', ['#000000', '#0E0E10', '#18181B', '#26262B', '#F4F4F6', '#A3A3AD', '#6C6C76'],
    ['#8B93FF', '#3DDC97', '#5AB0FF', '#FFB547', '#FF6B81', '#B98CFF', '#3FD8DE', '#FF7AC6'], '#3AB4F2', 'Default'),
  T('oled-mono', 'OLED Mono', 'dark', ['#000000', '#0E0E10', '#18181B', '#26262B', '#F4F4F6', '#A3A3AD', '#6C6C76'],
    Array(8).fill('#E4E4E7'), '#E4E4E7', undefined, true),
  T('midnight', 'Midnight', 'dark', ['#13151E', '#1C1F2B', '#242836', '#2C3040', '#E8EAF2', '#A6ABBE', '#6E7389'],
    ['#9BA3FF', '#6FDDB0', '#7DB6FF', '#F5B963', '#FF97A7', '#C6A2FF', '#6ADCDF', '#FF9FD2'], '#4FB8EC'),
  T('graphite', 'Graphite', 'dark', ['#1A1A1C', '#232326', '#2C2C30', '#37373C', '#EDEDEF', '#A9A9B0', '#75757D'],
    ['#8E95F0', '#4FCB91', '#63A9EE', '#EDB25A', '#EE7485', '#B395F0', '#4CC9CE', '#EC83BF'], '#3AB4F2'),
  T('fjord', 'Fjord', 'dark', ['#232A36', '#2C3442', '#343D4D', '#3E4859', '#ECEFF4', '#B4BCCB', '#7E889A'],
    ['#88A6E0', '#A3D49A', '#81C8D8', '#EBCB8B', '#E48A92', '#C29BD0', '#8FD0C9', '#D99AC0'], '#88C0D0', undefined, true),
  T('forest', 'Forest', 'dark', ['#0F1A16', '#15231E', '#1C2E27', '#263A32', '#E6F0EB', '#A4B8AE', '#6E8479'],
    ['#9FB4FF', '#6FE3A8', '#7CC4F0', '#F2C46B', '#FF8F8F', '#C7A6FF', '#5ED9C6', '#F59BCB'], '#3AB4F2', undefined, true),
  T('high-contrast', 'High contrast', 'dark', ['#000000', '#000000', '#111111', '#FFFFFF', '#FFFFFF', '#E6E6E6', '#BDBDBD'],
    ['#A5ABFF', '#4DFFB0', '#6EC1FF', '#FFC94D', '#FF7A8C', '#D0A6FF', '#4DF4FA', '#FF8FD6'], '#4FC3F7', 'Accessibility', true),
  T('daylight', 'Daylight', 'light', ['#ECEEF4', '#FFFFFF', '#F3F4F8', '#E1E4EC', '#1D2030', '#575C70', '#8D92A5'],
    ['#4651D0', '#16805A', '#2C73C9', '#A9620D', '#B5324A', '#7A43C2', '#0E8A8F', '#C04F8E'], '#1793D1'),
  T('linen', 'Linen', 'light', ['#F2EFE9', '#FBFAF7', '#EEEAE2', '#E2DCD1', '#2A2620', '#625B50', '#948B7E'],
    ['#4B55C4', '#2E7D50', '#2F6FB0', '#A35F12', '#B23A44', '#7B4AB8', '#187F86', '#B3487F'], '#1793D1'),
];

export const DEFAULT_THEME_ID = 'oled';
export const DEFAULT_LIGHT_THEME_ID = 'daylight';

export const HUE_LABEL_KEYS: Record<HueKey, string> = {
  ov: 'overview',
  term: 'terminal',
  file: 'files',
  log: 'logs',
  svc: 'services',
  sw: 'software',
  usr: 'users',
  plg: 'plugins',
};

/** Known distro colours for the "preview another distro" chips. */
export const DISTRO_PRESETS: { id: string; name: string; color: string }[] = [
  { id: 'arch', name: 'Arch', color: '#1793D1' },
  { id: 'ubuntu', name: 'Ubuntu', color: '#E95420' },
  { id: 'linuxmint', name: 'Mint', color: '#87CF3E' },
  { id: 'fedora', name: 'Fedora', color: '#51A2DA' },
  { id: 'debian', name: 'Debian', color: '#D70A53' },
  { id: 'opensuse', name: 'openSUSE', color: '#73BA25' },
  { id: 'manjaro', name: 'Manjaro', color: '#35BF5C' },
  { id: 'pop', name: 'Pop!_OS', color: '#48B9C7' },
  { id: 'endeavouros', name: 'EndeavourOS', color: '#7F3FBF' },
];
