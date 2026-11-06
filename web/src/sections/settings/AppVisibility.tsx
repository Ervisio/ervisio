import { usePrefs, useSession } from '../../api';
import { usePlugins } from '../../plugins';
import { useT } from '../../i18n';
import { SECTIONS } from '../../sections';
import { Switch } from '../../ui';
import { sectionShown, type ShellMode } from '../../shell/appVisibility';
import { usePrefSave } from './save';

export function AppVisibility() {
  const { prefs } = usePrefs();
  const { session, host } = useSession();
  const { plugins } = usePlugins();
  const save = usePrefSave();
  const t = useT('settings');
  const ts = useT('shell');
  const mode: ShellMode = (prefs['shell.mode'] ?? host?.defaultShell) === 'ervisios' ? 'ervisios' : 'classic';
  const key = `apps.visible.${mode}`;
  const sections = SECTIONS.filter(section => !['overview', 'settings'].includes(section.id) && !(mode === 'ervisios' && section.desktopHidden) && (!section.platforms || section.platforms.includes(session?.os ?? 'linux')));
  return <div className="st-app-visibility">
    <p>{t(mode === 'classic' ? 'apps.classicHint' : 'apps.desktopHint')}</p>
    {sections.map(section => <label key={section.id}><span>{ts(section.titleKey)}</span><Switch aria-label={ts(section.titleKey)} checked={sectionShown(section, prefs, mode)} onChange={on => save(key, { ...prefs[key], [section.id]: on }, t('apps.title'))} /></label>)}
    {plugins.some(plugin => plugin.id === 'ai') && <label><span>{t('apps.floatingAI')}</span><Switch aria-label={t('apps.floatingAI')} checked={prefs[`assistant.floating.${mode}`] ?? mode === 'classic'} onChange={on => save(`assistant.floating.${mode}`, on, t('apps.floatingAI'))} /></label>}
  </div>;
}
