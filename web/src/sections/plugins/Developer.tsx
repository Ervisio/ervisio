import { useState } from 'react';
import { call } from '../../api';
import { pluginLogoUrl } from '../../plugins';
import { useT } from '../../i18n';
import { Button, Icon, Input, toast } from '../../ui';
import { errMsg } from './data';
import { Tile } from './Parts';
import type { PluginInfo } from './types';

interface DevInfo {
  path: string;
  id: string;
  name: string;
  linked: boolean;
  note?: string;
}

export const DOCS_URL = 'https://github.com/Ervisio/plugin-sdk/blob/main/docs/sdk.md';

export function Developer({ plugins, refresh }: { plugins: PluginInfo[]; refresh(reloadLoader?: boolean): Promise<void> }) {
  const t = useT('plugins');
  const [path, setPath] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const dev = plugins.filter((p) => p.location === 'dev');

  const load = async () => {
    setBusy(true);
    setError('');
    try {
      const r = await call<DevInfo>('plugins.loadDev', { path: path.trim() });
      toast.ok(t('dev.loaded', { name: r.name }), r.note);
      setPath('');
      await refresh(true);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="plugins-devwrap">
      <div className="plugins-dev">
        <Tile icon="code" color="term" />
        <div className="plugins-dev-tx">
          <b>{t('dev.title')}</b>
          <small>{t('dev.text')} <code>~/projects/ervisio-plugin-zfs</code></small>
        </div>
        <div className="plugins-dev-act">
          <a className="ui-btn" href={DOCS_URL} target="_blank" rel="noreferrer"><Icon name="externallink" />{t('dev.docs')}</a>
        </div>
      </div>
      <form
        className="plugins-dev-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (path.trim()) void load();
        }}
      >
        <Input label={t('dev.path')} mono placeholder="~/projects/my-plugin" value={path} onChange={(e) => setPath(e.target.value)} error={error || undefined} hint={error ? undefined : t('dev.pathHint')} />
        <Button type="submit" variant="primary" icon="download" loading={busy} disabled={!path.trim()}>{t('dev.load')}</Button>
      </form>
      {dev.length > 0 && (
        <>
          <p className="plugins-h4">{t('dev.loadedList')}</p>
          <div className="plugins-devlist">
            {dev.map((p) => (
              <div key={p.id} className="plugins-devrow">
                <Tile icon={p.icon} logo={pluginLogoUrl(p)} color={p.color} size="sm" />
                <div className="plugins-dev-tx"><b>{p.name}</b><small className="mono">{p.dir}</small></div>
                <Button size="sm" icon="refresh" onClick={() => void refresh(true).then(() => toast.ok(t('dev.reloaded', { name: p.name })))}>{t('dev.reload')}</Button>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
