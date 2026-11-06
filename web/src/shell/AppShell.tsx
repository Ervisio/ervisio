import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { Name } from '../brand';
import { useT } from '../i18n';
import { SECTIONS, useSectionBackgroundHooks } from '../sections';
import { Button, EmptyState, Skeleton } from '../ui';
import { CommandPalette } from './CommandPalette';
import { usePlugins } from '../plugins';
import { UpdateNotifier } from '../sections/settings/UpdateNotifier';
import { usePrefs, useSession } from '../api';
import { useShellCommands } from './useShellCommands';
import { EditorDraftsProvider } from '../sections/editor/DraftsProvider';
import './shell.css';

const DesktopShell = lazy(() =>
  import('../desktop/DesktopShell').then((m) => ({ default: m.DesktopShell })),
);
const ClassicShell = lazy(() => import('./ClassicShell'));

function usePaletteShortcut(toggle: () => void) {
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        toggle();
      }
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, [toggle]);
}

function useDocumentTitle(mode: 'classic' | 'ervisios') {
  const t = useT('shell');
  const { pathname } = useLocation();
  const { railPages } = usePlugins();
  useEffect(() => {
    if (mode === 'ervisios') {
      document.title = Name;
      return;
    }
    const sec = SECTIONS.find((s) =>
      s.path === '/' ? pathname === '/' : pathname.startsWith(s.path),
    );
    const pg = pathname.startsWith('/p/')
      ? railPages.find((p) => pathname === `/p/${p.plugin}/${p.page}`)
      : undefined;
    const title = pg?.title ?? (sec ? t(sec.titleKey) : '');
    document.title = title ? `${title} · ${Name}` : Name;
  }, [pathname, railPages, t, mode]);
}

export function AppShell() {
  const [palette, setPalette] = useState(false);
  const { prefs, ready, error, retry } = usePrefs();
  const t = useT('shell');
  const { host, session } = useSession();
  const mode = prefs['shell.mode'] ?? host?.defaultShell ?? 'classic';
  useShellCommands();
  useSectionBackgroundHooks();
  usePaletteShortcut(useCallback(() => setPalette((o) => !o), []));
  useDocumentTitle(mode);

  if (!ready)
    return (
      <div className="app-loading">
        {error ? (
          <>
            <EmptyState icon="alert" title={t('desktop.prefsFailed')} text={error.message} />
            <Button onClick={retry}>{t('desktop.retry')}</Button>
          </>
        ) : (
          <Skeleton lines={5} />
        )}
      </div>
    );
  return (
    <EditorDraftsProvider key={session?.user}>
      <Suspense
        fallback={
          <div className="app-loading">
            <Skeleton lines={5} />
          </div>
        }
      >
        {mode === 'ervisios' ? (
          <DesktopShell key={session?.user} />
        ) : (
          <ClassicShell onSearch={() => setPalette(true)} />
        )}
      </Suspense>
      <CommandPalette open={palette} onClose={() => setPalette(false)} />
      <UpdateNotifier />
    </EditorDraftsProvider>
  );
}
