import type { Prefs } from '../api/prefs';
import type { SectionDef } from '../sections';

export type ShellMode = 'classic' | 'ervisios';
export function sectionShown(section: SectionDef, prefs: Prefs, mode: ShellMode): boolean {
  if (section.id === 'settings' || section.id === 'overview') return true;
  if (mode === 'ervisios' && section.desktopHidden) return false;
  const override = prefs[`apps.visible.${mode}`]?.[section.id];
  return typeof override === 'boolean' ? override : mode === 'ervisios' || section.rail;
}
