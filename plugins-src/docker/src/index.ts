/**
 * Entry of the Docker plugin (SDK v3). Order matters: the SDK and React are stored first, because every component
 * reads them through getSdk() and the `react` shim.
 */
import { createElement } from 'react';
import { setReact } from './react-shim';
import { setSdk, type PluginSDK } from './sdk';
import { registerAllStrings, t } from './i18n';
import { injectStyles } from './styles';
import { registerDockerIcons } from './ui/icons';
import { App } from './shell/App';
import { ContainersWidget } from './views/ContainersWidget';
import { EmptyState } from './kit';

function TooOld() {
  return createElement(EmptyState, { icon: 'alert', hue: 'svc', title: t('shell.oldSdk.title'), text: t('shell.oldSdk.text') });
}

export default function activate(sdk: PluginSDK): void {
  setSdk(sdk);
  setReact(sdk.react);
  registerAllStrings();
  injectStyles();
  registerDockerIcons();
  const ok = sdk.version >= 3;
  sdk.registerPage('docker', ok ? App : TooOld);
  sdk.registerWidget({ id: 'containers', render: ok ? ContainersWidget : TooOld });
}
