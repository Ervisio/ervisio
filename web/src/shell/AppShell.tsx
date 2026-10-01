import { Suspense, useEffect, useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Name } from '../brand';
import { useT } from '../i18n';
import { SECTIONS, useSectionBackgroundHooks } from '../sections';
import { Skeleton } from '../ui';
import { CommandPalette } from './CommandPalette';
import { Dock } from './Dock';
import { Rail } from './Rail';
import { TopBar } from './TopBar';
import { useFocusActive } from './focus';
import { usePlugins } from '../plugins';
import './shell.css';

export function AppShell() {
  const t = useT('shell');
  const loc = useLocation();
  const { railPages } = usePlugins();
  const [palette, setPalette] = useState(false);
  const focus = useFocusActive();
  useSectionBackgroundHooks();

  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPalette((o) => !o);
      }
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, []);

  useEffect(() => {
    const sec = SECTIONS.find((s) => (s.path === '/' ? loc.pathname === '/' : loc.pathname.startsWith(s.path)));
    const pg = loc.pathname.startsWith('/p/') ? railPages.find((p) => loc.pathname === `/p/${p.plugin}/${p.page}`) : undefined;
    const title = pg?.title ?? (sec ? t(sec.titleKey) : '');
    document.title = title ? `${title} · ${Name}` : Name;
  }, [loc.pathname, railPages, t]);

  return (
    <div className={`app${focus ? ' app--focus' : ''}`}>
      {!focus && <Rail />}
      <div className="app-main">
        {!focus && <TopBar onSearch={() => setPalette(true)} />}
        <main className="app-content" id="main">
          <Suspense fallback={<div style={{ padding: 28 }}><Skeleton width="30%" height={26} /><div style={{ height: 18 }} /><Skeleton lines={5} /></div>}>
            <Outlet />
          </Suspense>
        </main>
      </div>
      {!focus && <Dock />}
      <CommandPalette open={palette} onClose={() => setPalette(false)} />
    </div>
  );
}
