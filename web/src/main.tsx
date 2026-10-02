import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import '@fontsource/figtree/400.css';
import '@fontsource/figtree/500.css';
import '@fontsource/figtree/600.css';
import '@fontsource/figtree/700.css';
import '@fontsource/figtree/800.css';
import '@fontsource/jetbrains-mono/400.css';
import '@fontsource/jetbrains-mono/500.css';
import './styles/tokens.css';
import './ui';
import App from './App';
import { migrateStorage } from './lib/storageMigration';
import { PrefsProvider, SessionProvider } from './api';
import { I18nProvider } from './i18n';
import { PluginsProvider } from './plugins';
import { UnlockHost } from './shell/UnlockHost';
import { ThemeProvider } from './theme';
import { ToastViewport } from './ui';

// This browser's settings from LinuxAdmin (Ervisio's former name) move to Ervisio's keys once, before anything reads them.
migrateStorage(typeof localStorage === 'undefined' ? null : localStorage);

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <SessionProvider>
        <PrefsProvider>
          <ThemeProvider>
            <I18nProvider>
              <PluginsProvider>
                <App />
                <UnlockHost />
                <ToastViewport />
              </PluginsProvider>
            </I18nProvider>
          </ThemeProvider>
        </PrefsProvider>
      </SessionProvider>
    </BrowserRouter>
  </StrictMode>,
);
