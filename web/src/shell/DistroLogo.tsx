import type { SVGAttributes } from 'react';

/** Simple, recognisable marks for the common distributions. Tux is the fallback. Everything uses currentColor. */
const S = { fill: 'none', stroke: 'currentColor', strokeWidth: 2.2, strokeLinecap: 'round', strokeLinejoin: 'round' } as const;

type Art = () => JSX.Element;
const ART: Record<string, Art> = {
  arch: () => (
    <path fill="currentColor" d="M11.39.605C10.376 3.092 9.764 4.72 8.635 7.132c.693.734 1.543 1.589 2.923 2.554-1.484-.61-2.496-1.224-3.252-1.86C6.86 10.842 4.596 15.138 0 23.395c3.612-2.085 6.412-3.37 9.021-3.862a6.61 6.61 0 0 1-.171-1.547l.003-.115c.058-2.315 1.261-4.095 2.687-3.973 1.426.12 2.534 2.096 2.478 4.409a6.52 6.52 0 0 1-.146 1.243c2.58.505 5.352 1.787 8.914 3.844-.702-1.293-1.33-2.459-1.929-3.57-.943-.73-1.926-1.682-3.933-2.713 1.38.359 2.367.772 3.137 1.234-6.09-11.334-6.582-12.84-8.67-17.74z" />
  ),
  ubuntu: () => (
    <g>
      <circle cx="12" cy="12" r="7.4" {...S} strokeWidth={2.4} />
      <circle cx="4.6" cy="12" r="2.6" fill="currentColor" />
      <circle cx="15.7" cy="5.6" r="2.6" fill="currentColor" />
      <circle cx="15.7" cy="18.4" r="2.6" fill="currentColor" />
    </g>
  ),
  debian: () => (
    <path {...S} d="M15.8 4.6C10.2 2.8 5 7.2 6.4 12.6c1.2 4.4 7 5.6 9.3 2.2 1.8-2.6.7-6.1-2.2-6.6-2.4-.4-4.2 1.6-3.5 3.5.4 1.2 2 1.6 2.8.9" />
  ),
  fedora: () => (
    <g>
      <path {...S} d="M20.5 12A8.5 8.5 0 1 1 12 3.5h4" />
      <path {...S} d="M14.6 7.4h-1.5A2 2 0 0 0 11.1 9.4V17M8.6 11.6h5.4" />
    </g>
  ),
  linuxmint: () => (
    <g>
      <path {...S} d="M4 8a4 4 0 0 1 4-4h12v12a4 4 0 0 1-4 4H4z" />
      <path {...S} d="M8 16.5V9.5M8 12a2.2 2.2 0 0 1 4.4 0v4.5M12.4 12a2.2 2.2 0 0 1 4.4 0v4.5" />
    </g>
  ),
  opensuse: () => (
    <g>
      <circle cx="12" cy="12" r="8.6" {...S} />
      <circle cx="12" cy="12" r="3" fill="currentColor" />
      <path {...S} d="M20.4 9.5c1.2.6 1.8 1.7 1.2 2.9" />
    </g>
  ),
  manjaro: () => (
    <g fill="currentColor">
      <rect x="3" y="3" width="18" height="5.2" rx="1" />
      <rect x="3" y="3" width="5.6" height="18" rx="1" />
      <rect x="9.2" y="9.6" width="5.6" height="11.4" rx="1" />
      <rect x="15.4" y="3" width="5.6" height="18" rx="1" />
    </g>
  ),
  pop: () => (
    <g>
      <rect x="3" y="3" width="18" height="18" rx="6" {...S} />
      <path {...S} d="M9.4 17V8h3.3a2.6 2.6 0 0 1 0 5.2H9.4" />
    </g>
  ),
  endeavouros: () => (
    <g>
      <path {...S} d="M3.5 19.5 12 4l8.5 15.5z" />
      <path {...S} d="M8 16.5c2.2-2.8 5.8-2.8 8 0" />
    </g>
  ),
  // Windows: flat four tiles (11), tiles in perspective (10 / 8), waving flag (7 and older).
  'windows-11': () => (
    <g fill="currentColor">
      <rect x="3" y="3" width="8.5" height="8.5" rx=".6" />
      <rect x="12.5" y="3" width="8.5" height="8.5" rx=".6" />
      <rect x="3" y="12.5" width="8.5" height="8.5" rx=".6" />
      <rect x="12.5" y="12.5" width="8.5" height="8.5" rx=".6" />
    </g>
  ),
  'windows-10': () => (
    <path fill="currentColor" d="M2.5 5.2 10.4 4.1v7.5H2.5zM11.4 3.9 21.5 2.5v9.1H11.4zM2.5 12.5h7.9V20L2.5 18.9zM11.4 12.5h10.1v9L11.4 20.2z" />
  ),
  'windows-8': () => (
    <path fill="currentColor" d="M3 5.6 10.4 4.6v7H3zM11.4 4.5 21 3.2v8.4h-9.6zM3 12.6h7.4v7L3 18.5zM11.4 12.6H21v8.3l-9.6-1.3z" />
  ),
  'windows-7': () => (
    <g fill="currentColor">
      <path d="M3.2 5.6c2.4-1.1 4.8-1.1 7.2 0l-1.1 6.2c-2.4-1.1-4.8-1.1-7.2 0z" />
      <path d="M11.4 6c2.4 1.1 4.8 1.1 7.2 0l-1.1 6.2c-2.4 1.1-4.8 1.1-7.2 0z" />
      <path d="M2 12.8c2.4-1.1 4.8-1.1 7.2 0l-1.1 6.2c-2.4-1.1-4.8-1.1-7.2 0z" />
      <path d="M10.2 13.2c2.4 1.1 4.8 1.1 7.2 0l-1.1 6.2c-2.4 1.1-4.8 1.1-7.2 0z" />
    </g>
  ),
  tux: () => (
    <g>
      <path fill="currentColor" d="M12 2.4c-2.6 0-4 2.2-4 5 0 1.7-.6 2.6-1.7 4.2C5 13.5 4 15.4 4 17.2c0 1.4.8 2 2 2.3.4 1.4 1.5 2.4 3 2.4h6c1.5 0 2.6-1 3-2.4 1.2-.3 2-.9 2-2.3 0-1.8-1-3.7-2.3-5.6C16.6 10 16 9.1 16 7.4c0-2.8-1.4-5-4-5z" />
      <ellipse cx="12" cy="15.3" rx="3.4" ry="4.6" fill="#fff" opacity=".88" />
      <circle cx="10.4" cy="7.1" r="1" fill="#fff" />
      <circle cx="13.6" cy="7.1" r="1" fill="#fff" />
      <path d="M10.2 9.2c.5.5 1 .8 1.8.8s1.3-.3 1.8-.8c-.4 1-1.100 1.600-1.800 1.600s-1.400-.6-1.800-1.600z" fill="#FFB547" />
    </g>
  ),
};

const ALIASES: Record<string, string> = {
  archlinux: 'arch', arch: 'arch', manjaro: 'manjaro', 'manjaro-linux': 'manjaro',
  ubuntu: 'ubuntu', 'ubuntu-logo': 'ubuntu', 'distributor-logo-ubuntu': 'ubuntu',
  debian: 'debian', 'debian-logo': 'debian', raspbian: 'debian',
  fedora: 'fedora', 'fedora-logo': 'fedora', 'fedora-logo-icon': 'fedora', rhel: 'fedora', centos: 'fedora',
  linuxmint: 'linuxmint', mint: 'linuxmint', 'linuxmint-logo': 'linuxmint', 'distributor-logo-linuxmint': 'linuxmint',
  opensuse: 'opensuse', 'opensuse-leap': 'opensuse', 'opensuse-tumbleweed': 'opensuse', 'distributor-logo-opensuse': 'opensuse', suse: 'opensuse',
  pop: 'pop', pop_os: 'pop', 'pop-os': 'pop', 'distributor-logo-pop_os': 'pop',
  'windows-11': 'windows-11', 'windows-10': 'windows-10', 'windows-8': 'windows-8', 'windows-7': 'windows-7', windows: 'windows-11',
  endeavouros: 'endeavouros', 'endeavouros-logo': 'endeavouros', 'distributor-logo-endeavouros': 'endeavouros',
};

/** Maps an os-release ID / LOGO value to one of the bundled marks. */
export function distroKey(id?: string, logo?: string): keyof typeof ART {
  for (const raw of [logo, id]) {
    const k = raw?.toLowerCase().replace(/\s+/g, '-');
    if (!k) continue;
    if (ALIASES[k]) return ALIASES[k];
    const hit = Object.keys(ALIASES).find((a) => k.includes(a));
    if (hit) return ALIASES[hit];
  }
  return 'tux';
}

export function DistroLogo({ id, logo, url, ...rest }: { id?: string; logo?: string; /** server-provided icon, used only when no bundled mark matches */ url?: string } & SVGAttributes<SVGSVGElement>) {
  const key = distroKey(id, logo);
  if (key === 'tux' && url) return <img src={url} alt="" className={(rest as { className?: string }).className} style={{ objectFit: 'contain' }} />;
  const Art = ART[key];
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" {...rest}>
      <Art />
    </svg>
  );
}
