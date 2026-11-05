/** Virtual desktop metadata. These folders are never paths on the host filesystem. */
export interface DesktopFolder {
  id: string;
  name: string;
  parent: string | null;
}
export interface DesktopShortcut {
  id: string;
  app: string;
  folder: string | null;
}
export interface WindowBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}
export interface DesktopData {
  version: 1;
  folders: DesktopFolder[];
  shortcuts: DesktopShortcut[];
  pinned: string[];
  wallpaper: 'bloom' | 'mint' | 'sunset' | 'plain';
  bounds: Record<string, WindowBounds>;
}
export const DEFAULT_APPS = ['this-pc', 'files', 'terminal', 'plugins', 'settings'];
const text = (x: unknown, max = 160): x is string =>
  typeof x === 'string' && x.length > 0 && x.length <= max;
const record = (x: unknown): x is Record<string, unknown> =>
  !!x && typeof x === 'object' && !Array.isArray(x);

export function defaultDesktop(apps: string[]): DesktopData {
  const initial = DEFAULT_APPS.concat(apps.find((a) => a.startsWith('p:ai/')) ?? []).filter((a) =>
    apps.includes(a),
  );
  return {
    version: 1,
    folders: [],
    shortcuts: initial.map((app) => ({ id: `shortcut:${app}`, app, folder: null })),
    pinned: ['files', 'terminal', 'plugins', 'settings'].filter((a) => apps.includes(a)),
    wallpaper: 'bloom',
    bounds: {},
  };
}

/** Validate stored JSON without removing shortcuts for temporarily unavailable plugins. */
export function readDesktop(value: unknown, apps: string[]): DesktopData {
  if (!record(value) || value.version !== 1) return defaultDesktop(apps);
  const folders: DesktopFolder[] = [];
  for (const f of (Array.isArray(value.folders) ? value.folders : []).slice(0, 256)) {
    if (record(f) && text(f.id) && text(f.name, 80) && !folders.some((x) => x.id === f.id))
      folders.push({
        id: f.id,
        name: f.name.trim() || 'Folder',
        parent: text(f.parent) ? f.parent : null,
      });
  }
  const ids = new Set(folders.map((f) => f.id));
  for (const f of folders) {
    if (f.parent && (!ids.has(f.parent) || f.parent === f.id)) f.parent = null;
    const seen = new Set([f.id]);
    let parent = f.parent;
    while (parent) {
      if (seen.has(parent)) {
        f.parent = null;
        break;
      }
      seen.add(parent);
      parent = folders.find((x) => x.id === parent)?.parent ?? null;
    }
  }
  const shortcuts: DesktopShortcut[] = [];
  for (const s of (Array.isArray(value.shortcuts) ? value.shortcuts : []).slice(0, 1024)) {
    if (
      record(s) &&
      text(s.id) &&
      text(s.app) &&
      !ids.has(s.id) &&
      !shortcuts.some((x) => x.id === s.id)
    )
      shortcuts.push({
        id: s.id,
        app: s.app,
        folder: text(s.folder) && ids.has(s.folder) ? s.folder : null,
      });
  }
  const bounds: Record<string, WindowBounds> = Object.create(null);
  if (record(value.bounds))
    for (const [id, b] of Object.entries(value.bounds).slice(0, 256)) {
      if (!record(b)) continue;
      const { x, y, width, height } = b;
      if ([x, y, width, height].every((n) => typeof n === 'number' && Number.isFinite(n)))
        bounds[id] = {
          x: Math.max(0, Math.min(8000, x as number)),
          y: Math.max(0, Math.min(8000, y as number)),
          width: Math.max(320, Math.min(8000, width as number)),
          height: Math.max(240, Math.min(8000, height as number)),
        };
    }
  return {
    version: 1,
    folders,
    shortcuts,
    pinned: Array.isArray(value.pinned)
      ? [...new Set(value.pinned.filter((x): x is string => text(x)))].slice(0, 32)
      : [],
    wallpaper: ['bloom', 'mint', 'sunset', 'plain'].includes(String(value.wallpaper))
      ? (value.wallpaper as DesktopData['wallpaper'])
      : 'bloom',
    bounds,
  };
}

export function addShortcut(data: DesktopData, app: string, folder: string | null): DesktopData {
  if (!text(app) || data.shortcuts.length >= 1024) return data;
  if (folder && !data.folders.some((f) => f.id === folder)) return data;
  if (data.shortcuts.some((s) => s.app === app && s.folder === folder)) return data;
  return { ...data, shortcuts: [...data.shortcuts, { id: crypto.randomUUID(), app, folder }] };
}

export function moveItem(data: DesktopData, id: string, target: string | null): DesktopData {
  if (target && !data.folders.some((f) => f.id === target)) return data;
  const folder = data.folders.find((f) => f.id === id);
  if (folder) {
    if (folder.parent === target) return data;
    const seen = new Set<string>();
    let p = target;
    while (p) {
      if (p === id || seen.has(p)) return data;
      seen.add(p);
      p = data.folders.find((f) => f.id === p)?.parent ?? null;
    }
    return {
      ...data,
      folders: data.folders.map((f) => (f.id === id ? { ...f, parent: target } : f)),
    };
  }
  const shortcut = data.shortcuts.find((s) => s.id === id);
  if (
    !shortcut ||
    shortcut.folder === target ||
    data.shortcuts.some((s) => s.id !== id && s.app === shortcut.app && s.folder === target)
  )
    return data;
  return {
    ...data,
    shortcuts: data.shortcuts.map((s) => (s.id === id ? { ...s, folder: target } : s)),
  };
}

/** Removing an organizer keeps every contained shortcut and child folder. */
export function removeFolder(data: DesktopData, id: string): DesktopData {
  const folder = data.folders.find((f) => f.id === id);
  if (!folder) return data;
  return {
    ...data,
    folders: data.folders
      .filter((f) => f.id !== id)
      .map((f) => (f.parent === id ? { ...f, parent: folder.parent } : f)),
    shortcuts: data.shortcuts.map((s) => (s.folder === id ? { ...s, folder: folder.parent } : s)),
  };
}

export function clampBounds(b: WindowBounds, width: number, height: number): WindowBounds {
  const viewportWidth = Number.isFinite(width) ? Math.max(1, width) : 1024;
  const viewportHeight = Number.isFinite(height) ? Math.max(1, height) : 768;
  const maxWidth = Math.max(1, viewportWidth - 16),
    maxHeight = Math.max(1, viewportHeight - 16);
  const minWidth = Math.min(320, maxWidth),
    minHeight = Math.min(240, maxHeight);
  const w = Math.max(minWidth, Math.min(Number.isFinite(b.width) ? b.width : minWidth, maxWidth));
  const h = Math.max(
    minHeight,
    Math.min(Number.isFinite(b.height) ? b.height : minHeight, maxHeight),
  );
  return {
    width: w,
    height: h,
    x: Math.max(0, Math.min(Number.isFinite(b.x) ? b.x : 0, viewportWidth - w)),
    y: Math.max(0, Math.min(Number.isFinite(b.y) ? b.y : 0, viewportHeight - h)),
  };
}
