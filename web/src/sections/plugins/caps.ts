import type { Capabilities, Contributes, Command, Folder } from './types';

type T = (key: string, vars?: Record<string, string | number>) => string;

export interface CapItem {
  key: string;
  hue: 'svc' | 'log' | 'file' | 'term' | 'usr' | 'sw';
  icon: string;
  title: string;
  text?: string;
  /** Rendered as code chips. */
  codes?: string[];
}

const uniq = <X,>(l: X[]) => [...new Set(l)];

/** Program names of a command list: ["docker", "systemctl"]. */
export function programs(cmds: Command[]): string[] {
  return uniq(cmds.map((c) => c.argv[0].split('/').pop() ?? c.argv[0]));
}

/** A files entry as an object. */
export const folderOf = (f: Folder) => (typeof f === 'string' ? { path: f, admin: false, adminUnlessGroup: undefined, create: false } : f);

export const httpApis = (c: Capabilities) => c.http ?? [];

/** Anything the plugin may use with administrator rights: commands, HTTP APIs or folders. */
export const runsRoot = (c: Capabilities) =>
  c.commands.some((x) => x.admin) || httpApis(c).some((h) => h.admin) || [...c.files.read, ...c.files.write].some((f) => folderOf(f).admin);

const groupNote = (groups: (string | undefined)[], t: T) => {
  const g = uniq(groups.filter(Boolean) as string[]);
  return g.length ? ' ' + t('cap.rootGroup', { groups: g.join(', ') }) : '';
};

/** Human description of what a capability set allows, used by the side panel and the install dialog. */
export function describeCaps(caps: Capabilities, contributes: Contributes, t: T): CapItem[] {
  const out: CapItem[] = [];
  const pty = caps.commands.filter((c) => c.pty);
  const root = caps.commands.filter((c) => c.admin && !c.pty);
  const user = caps.commands.filter((c) => !c.admin && !c.pty);
  if (root.length) {
    out.push({
      key: 'root',
      hue: 'svc',
      icon: 'shield',
      title: t('cap.root'),
      text: t('cap.rootText', { count: root.length }) + groupNote(root.map((c) => c.adminUnlessGroup), t),
      codes: programs(root),
    });
  }
  if (user.length) {
    out.push({ key: 'user', hue: 'term', icon: 'terminal', title: t('cap.user'), text: t('cap.userText', { count: user.length }), codes: programs(user) });
  }
  if (pty.length) {
    const adm = pty.filter((c) => c.admin);
    out.push({
      key: 'pty',
      hue: adm.length ? 'svc' : 'term',
      icon: 'terminal',
      title: t(adm.length ? 'cap.ptyRoot' : 'cap.pty'),
      text: t('cap.ptyText', { count: pty.length }) + groupNote(adm.map((c) => c.adminUnlessGroup), t),
      codes: uniq(pty.map((c) => c.argv.map((a) => a.split('/').pop() ?? a).slice(0, 3).join(' '))),
    });
  }
  for (const h of httpApis(caps)) {
    const rules = h.rules?.length ?? 0;
    out.push({
      key: `http-${h.name}`,
      hue: h.admin ? 'svc' : 'log',
      icon: 'link',
      title: t(h.admin ? 'cap.httpRoot' : 'cap.http'),
      text:
        t('cap.httpText', { count: rules }) +
        groupNote([h.adminUnlessGroup], t) +
        (h.admin ? ' ' + t('cap.httpRootWarn') : ''),
      codes: [h.socket],
    });
  }
  if (caps.sockets.length) out.push({ key: 'sockets', hue: 'log', icon: 'link', title: t('cap.sockets'), text: t('cap.socketsText'), codes: caps.sockets });
  const plain = (l: Folder[]) => l.map(folderOf).filter((f) => !f.admin).map((f) => f.path);
  const admin = (l: Folder[]) => l.map(folderOf).filter((f) => f.admin);
  const rd = plain(caps.files.read);
  const wr = plain(caps.files.write);
  if (rd.length) out.push({ key: 'read', hue: 'file', icon: 'eye', title: t('cap.read'), codes: rd });
  if (wr.length) out.push({ key: 'write', hue: 'file', icon: 'edit', title: t('cap.write'), codes: wr });
  const ard = admin(caps.files.read);
  const awr = admin(caps.files.write);
  if (ard.length) out.push({ key: 'adminRead', hue: 'svc', icon: 'eye', title: t('cap.readRoot'), text: groupNote(ard.map((f) => f.adminUnlessGroup), t).trim() || undefined, codes: ard.map((f) => f.path) });
  if (awr.length) out.push({ key: 'adminWrite', hue: 'svc', icon: 'edit', title: t('cap.writeRoot'), text: groupNote(awr.map((f) => f.adminUnlessGroup), t).trim() || undefined, codes: awr.map((f) => f.path) });
  if (caps.network.length) out.push({ key: 'network', hue: 'sw', icon: 'globe', title: t('cap.network'), text: t('cap.networkText'), codes: caps.network });
  const ui = uiSummary(contributes, t);
  if (ui) out.push({ key: 'ui', hue: 'term', icon: 'grid', title: t('cap.ui'), text: ui });
  if (!out.length) out.push({ key: 'none', hue: 'term', icon: 'check', title: t('cap.none'), text: t('cap.noneText') });
  return out;
}

export function uiSummary(c: Contributes, t: T): string {
  const parts: string[] = [];
  if (c.pages.length) parts.push(t('cap.pages', { count: c.pages.length }));
  if (c.widgets.length) parts.push(t('cap.widgets', { count: c.widgets.length }));
  if (c.snippets.length) parts.push(t('cap.snippets', { count: c.snippets.length }));
  return parts.join(', ');
}

export function whoCanUse(groups: string[], t: T): { title: string; text: string } {
  if (!groups.length) return { title: t('who.everyone'), text: t('who.everyoneText') };
  return { title: t('who.groups', { groups: groups.join(', ') }), text: t('who.groupsText') };
}
