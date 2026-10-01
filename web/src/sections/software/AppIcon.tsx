import { useEffect, useState } from 'react';
import { call } from '../../api';

/** Loads real icons through software.icon (icon theme, pixmaps, flatpak exports), a few at a time. */
const cache = new Map<string, Promise<string | null>>();
const queue: (() => void)[] = [];
let active = 0;
const MAX = 4;

function pump() {
  while (active < MAX && queue.length) {
    active++;
    queue.shift()!();
  }
}

export function loadIcon(name: string, size: number): Promise<string | null> {
  const key = `${name}|${size}`;
  let p = cache.get(key);
  if (!p) {
    p = new Promise<string | null>((resolve) => {
      queue.push(() => {
        call<{ mime: string; data: string }>('software.icon', { name, size })
          .then((r) => resolve(`data:${r.mime};base64,${r.data}`))
          .catch(() => resolve(null))
          .finally(() => {
            active--;
            pump();
          });
      });
      pump();
    });
    cache.set(key, p);
  }
  return p;
}

const HUES = ['file', 'term', 'plg', 'log', 'ov', 'usr', 'svc', 'sw'] as const;
export const hueFor = (s: string) => {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return HUES[h % HUES.length];
};

/** Real icon when one exists, otherwise a coloured letter tile. */
export function AppIcon({ icon, label, size = 48 }: { icon?: string; label: string; size?: number }) {
  const [src, setSrc] = useState<string | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    setSrc(undefined);
    if (!icon) {
      setSrc(null);
      return;
    }
    void loadIcon(icon, size * 2).then((s) => live && setSrc(s));
    return () => {
      live = false;
    };
  }, [icon, size]);
  const hue = hueFor(label);
  const letter = (label.replace(/^[^A-Za-z0-9]+/, '')[0] ?? '?').toUpperCase();
  return (
    <span className={`sw-ic hue-${hue}`} style={{ width: size, height: size, fontSize: Math.round(size * 0.42) }} aria-hidden="true">
      {src ? <img src={src} alt="" draggable={false} /> : letter}
    </span>
  );
}
