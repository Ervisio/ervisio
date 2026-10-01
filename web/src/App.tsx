import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from './shell/AppShell';
import { PluginPage } from './plugins';
import { SECTIONS } from './sections';
import LoginPage from './pages/Login';
import { RequireAuth } from './pages/RequireAuth';
import { lazy } from 'react';

const KitPage = import.meta.env.DEV ? lazy(() => import('./pages/Kit')) : null;

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        {SECTIONS.map((s) => (
          <Route key={s.id} path={s.path === '/' ? '/' : `${s.path}/*`} element={<s.page />} />
        ))}
        <Route path="/p/:plugin/:page" element={<PluginPage />} />
        {KitPage && <Route path="/__kit" element={<KitPage />} />}
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
