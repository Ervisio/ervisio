import type { Capabilities, Contributes, Command } from './types';

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

export const runsRoot = (c: Capabilities) => c.commands.some((x) => x.admin);

/** Human description of what a capability set allows, used by the side panel and the install dialog. */
export function describeCaps(caps: Capabilities, contributes: Contributes, t: T): CapItem[] {
  const out: CapItem[] = [];
  const root = caps.commands.filter((c) => c.admin);
  const user = caps.commands.filter((c) => !c.admin);
  if (root.length) {
    const groups = uniq(root.map((c) => c.adminUnlessGroup).filter(Boolean) as string[]);
    out.push({
      key: 'root',
      hue: 'svc',
      icon: 'shield',
      title: t('cap.root'),
      text: t('cap.rootText', { count: root.length }) + (groups.length ? ' ' + t('cap.rootGroup', { groups: groups.join(', ') }) : ''),
      codes: programs(root),
    });
  }
  if (user.length) {
    out.push({ key: 'user', hue: 'term', icon: 'terminal', title: t('cap.user'), text: t('cap.userText', { count: user.length }), codes: programs(user) });
  }
  if (caps.sockets.length) out.push({ key: 'sockets', hue: 'log', icon: 'link', title: t('cap.sockets'), text: t('cap.socketsText'), codes: caps.sockets });
  if (caps.files.read.length) out.push({ key: 'read', hue: 'file', icon: 'eye', title: t('cap.read'), codes: caps.files.read });
  if (caps.files.write.length) out.push({ key: 'write', hue: 'file', icon: 'edit', title: t('cap.write'), codes: caps.files.write });
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
