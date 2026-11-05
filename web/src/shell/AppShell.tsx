import { Suspense, useCallback, useEffect, useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Name } from '../brand';
import { useT } from '../i18n';
import { SECTIONS, useSectionBackgroundHooks } from '../sections';
import { Button, EmptyState, Skeleton } from '../ui';
import { CommandPalette } from './CommandPalette';
import { Dock } from './Dock';
import { Rail } from './Rail';
import { TopBar } from './TopBar';
import { useFocusActive } from './focus';
import { usePlugins } from '../plugins';
import { UpdateNotifier } from '../sections/settings/UpdateNotifier';
import { usePrefs, useSession } from '../api';
import { DesktopShell } from '../desktop/DesktopShell';
import { useShellCommands } from './useShellCommands';
import './shell.css';

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

function useDocumentTitle() {
  const t = useT('shell');
  const { pathname } = useLocation();
  const { railPages } = usePlugins();
  useEffect(() => {
    const sec = SECTIONS.find((s) =>
      s.path === '/' ? pathname === '/' : pathname.startsWith(s.path),
    );
    const pg = pathname.startsWith('/p/')
      ? railPages.find((p) => pathname === `/p/${p.plugin}/${p.page}`)
      : undefined;
    const title = pg?.title ?? (sec ? t(sec.titleKey) : '');
    document.title = title ? `${title} · ${Name}` : Name;
  }, [pathname, railPages, t]);
}

export function AppShell() {
  const [palette, setPalette] = useState(false);
  const { prefs, ready, error, retry } = usePrefs();
  const t = useT('shell');
  const { host, session } = useSession();
  const mode = prefs['shell.mode'] ?? host?.defaultShell ?? 'classic';
  const focus = useFocusActive();
  useShellCommands();
  useSectionBackgroundHooks();
  usePaletteShortcut(useCallback(() => setPalette((o) => !o), []));
  useDocumentTitle();

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
  if (mode === 'ervisios')
    return (
      <>
        <DesktopShell key={session?.user} />
        <CommandPalette open={palette} onClose={() => setPalette(false)} />
        <UpdateNotifier />
      </>
    );

  return (
    <div className={`app${focus ? ' app--focus' : ''}`}>
      {!focus && <Rail />}
      <div className="app-main">
        {!focus && <TopBar onSearch={() => setPalette(true)} />}
        <main className="app-content" id="main">
          <Suspense
            fallback={
              <div style={{ padding: 28 }}>
                <Skeleton width="30%" height={26} />
                <div style={{ height: 18 }} />
                <Skeleton lines={5} />
              </div>
            }
          >
            <Outlet />
          </Suspense>
        </main>
      </div>
      {!focus && <Dock />}
      <CommandPalette open={palette} onClose={() => setPalette(false)} />
      <UpdateNotifier />
    </div>
  );
}
