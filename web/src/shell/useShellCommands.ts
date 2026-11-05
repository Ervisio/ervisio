import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { usePrefs, useSession } from '../api';
import { useT } from '../i18n';
import { usePlugins } from '../plugins';
import { SECTIONS } from '../sections';
import { useTheme } from '../theme';
import { parseShellCommand, setShellCommandHandler, type ShellApp } from './commands';
import { rememberLoginMode } from './loginMode';

const aliases: Record<string, string[]> = {
  overview: ['panoramica', 'dashboard'],
  files: ['esplora risorse', 'esplora file', 'file manager', 'file explorer', 'explorer', 'risorse'],
  settings: ['impostazioni', 'settings', 'configurazione'],
  terminal: ['terminale', 'terminal', 'console'],
  plugins: ['store', 'app store', 'negozio', 'plugin'],
  services: ['servizi', 'services'], logs: ['registri', 'logs'],
  software: ['software', 'programmi', 'aggiornamenti'], users: ['utenti', 'users'],
};

/** Registered once by AppShell. Local requests never ask an AI provider to interpret UI commands. */
export function useShellCommands() {
  const navigate = useNavigate();
  const { pages } = usePlugins();
  const { session, host } = useSession();
  const { prefs, set } = usePrefs();
  const t = useT('shell');
  const th = useTheme();
  useEffect(() => {
    if (session?.user) rememberLoginMode(session.user, (prefs['shell.mode'] ?? host?.defaultShell) === 'ervisios' ? 'ervisios' : 'classic');
  }, [session?.user, prefs, host?.defaultShell]);
  useEffect(() => {
    const desktop = (prefs['shell.mode'] ?? host?.defaultShell) === 'ervisios';
    const apps: ShellApp[] = [
      ...(desktop ? [{ id: 'this-pc', title: t('desktop.thisPC'), to: '/?desktop=pc', aliases: ['questo pc', 'this pc'] }] : []),
      ...SECTIONS.filter((s) => !s.platforms || s.platforms.includes(session?.os ?? 'linux')).map((s) => ({
        id: s.id, title: t(s.titleKey), to: s.path, aliases: aliases[s.id],
      })),
      ...pages.map((p) => ({ id: `${p.plugin}/${p.id}`, title: p.title, to: `/p/${p.plugin}/${p.id}`, aliases: [p.plugin, p.id] })),
    ];
    return setShellCommandHandler(async (text) => {
      const intent = parseShellCommand(text, apps);
      if (!intent) return { handled: false };
      switch (intent.kind) {
        case 'open':
          if (desktop) window.dispatchEvent(new CustomEvent('ervisio:shell-action', { detail: { action: 'open-app', to: intent.app.to } }));
          navigate(intent.app.to);
          return { handled: true, message: t('commands.opening', { name: intent.app.title }) };
        case 'theme': {
          const dark = intent.value === 'toggle' ? !th.isDark : intent.value === 'dark';
          if (dark !== th.isDark) th.toggleDark();
          return { handled: true, message: t(`commands.${dark ? 'dark' : 'light'}`) };
        }
        case 'mode':
          await set('shell.mode', intent.value);
          return { handled: true, message: t(`commands.${intent.value}`) };
        case 'window':
          if (!desktop) return { handled: false };
          window.dispatchEvent(new CustomEvent('ervisio:shell-action', { detail: { action: intent.action } }));
          return { handled: true, message: t('commands.done') };
      }
    });
  }, [navigate, pages, session?.os, host?.defaultShell, prefs, set, t, th]);
}
