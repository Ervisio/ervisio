import { Suspense } from 'react';
import { Outlet } from 'react-router-dom';
import { Skeleton } from '../ui';
import { Dock } from './Dock';
import { Rail } from './Rail';
import { TopBar } from './TopBar';
import { useFocusActive } from './focus';
import './classic.css';

/** Download classic navigation only when this account selects it. */
export default function ClassicShell({ onSearch }: { onSearch(): void }) {
  const focus = useFocusActive();
  return (
    <div className={`app${focus ? ' app--focus' : ''}`}>
      {!focus && <Rail />}
      <div className="app-main">
        {!focus && <TopBar onSearch={onSearch} />}
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
    </div>
  );
}
