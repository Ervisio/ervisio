import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { call } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, ConfirmDialog, Dialog, Icon, Panel, toast } from '../../ui';
import { errMsg } from './data';
import { CapList, H4, hueOf, WhoCanUse } from './Parts';
import type { PluginInfo } from './types';

interface Props {
  p: PluginInfo;
  onClose(): void;
  onToggle(on: boolean): void;
  onUpdate(p: PluginInfo): void;
  refresh(reloadLoader?: boolean): Promise<void>;
  busy: boolean;
}

/** Right side panel of the Installed and Updates tabs. */
export function DetailPanel({ p, onClose, onToggle, onUpdate, refresh, busy }: Props) {
  const t = useT('plugins');
  const nav = useNavigate();
  const [confirm, setConfirm] = useState(false);
  const [settings, setSettings] = useState(false);
  const [working, setWorking] = useState(false);
  const page = p.contributes.pages[0];

  const uninstall = async () => {
    setWorking(true);
    try {
      await call('plugins.uninstall', { id: p.id });
      toast.ok(t('removed', { name: p.name }));
      setConfirm(false);
      onClose();
      await refresh(true);
    } catch (e) {
      toast.err(t('removeFailed', { name: p.name }), errMsg(e));
    } finally {
      setWorking(false);
    }
  };
  const unload = async () => {
    setWorking(true);
    try {
      await call('plugins.unloadDev', { path: p.dir });
      toast.ok(t('unloaded', { name: p.name }));
      onClose();
      await refresh(true);
    } catch (e) {
      toast.err(t('removeFailed', { name: p.name }), errMsg(e));
    } finally {
      setWorking(false);
    }
  };

  return (
    <Panel
      inline
      open
      onClose={onClose}
      title={p.name}
      subtitle={`${p.author ? p.author + ', ' : ''}v${p.version}`}
      icon={p.icon}
      hue={hueOf(p.color)}
    >
      {p.error && <div className="plugins-note is-err">{p.error}</div>}
      {p.blocked && <div className="plugins-note is-warn">{t('blockedText')}</div>}
      {p.incompatible && <div className="plugins-note is-warn">{p.incompatible}</div>}
      {p.devUnsigned && <div className="plugins-note is-warn">{t('devUnsignedText')}</div>}
      {p.updateAvailable && (
        <div className="plugins-upd">
          <div>
            <b>{t('updateTitle', { version: p.updateAvailable.version })}</b>
            <small>
              {p.updateAvailable.notes ?? ''} {p.updateAvailable.newPermissions ? t('updateAsksMore') : t('updateNoMore')}
            </small>
          </div>
          <Button variant="primary" size="sm" disabled={busy || !p.updateAvailable.source} onClick={() => onUpdate(p)}>{t('update')}</Button>
        </div>
      )}
      <div className="plugins-acts">
        <Button icon="cog" onClick={() => setSettings(true)}>{t('settings')}</Button>
        <Button icon={p.enabled ? 'pause' : 'play'} disabled={busy || !!p.blocked || !!p.incompatible || !!p.error} onClick={() => onToggle(!p.enabled)}>{p.enabled ? t('disable') : t('enable')}</Button>
        {p.location === 'installed' && <Button variant="danger" icon="trash" onClick={() => setConfirm(true)}>{t('uninstall')}</Button>}
        {p.unloadable && <Button variant="danger" icon="close" loading={working} onClick={unload}>{t('unload')}</Button>}
      </div>
      {p.location === 'system' && <small className="plugins-muted">{t('systemPlugin')}</small>}
      <H4>{t('whatItCanDo')}</H4>
      <CapList item={p} />
      <H4>{t('whoCanUse')}</H4>
      <WhoCanUse groups={p.visibleTo.groups} />

      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        onConfirm={uninstall}
        title={t('uninstallTitle', { name: p.name })}
        description={t('uninstallText')}
        confirmLabel={t('uninstall')}
        confirmText={p.id}
      />
      <Dialog
        open={settings}
        onClose={() => setSettings(false)}
        title={t('settingsTitle', { name: p.name })}
        icon={p.icon}
        footer={
          <>
            {page && p.enabled && (
              <Button variant="primary" icon="right" onClick={() => { setSettings(false); nav(`/p/${p.id}/${page.id}`); }}>{t('openPage')}</Button>
            )}
            <Button onClick={() => setSettings(false)}>{t('close')}</Button>
          </>
        }
      >
        <dl className="plugins-kv">
          <dt>{t('kv.id')}</dt><dd className="mono">{p.id}</dd>
          <dt>{t('kv.version')}</dt><dd>{p.version}</dd>
          <dt>{t('kv.location')}</dt><dd>{t(`location.${p.location}`)}</dd>
          {p.dir && <><dt>{t('kv.folder')}</dt><dd className="mono">{p.dir}</dd></>}
          <dt>{t('kv.signature')}</dt>
          <dd>
            {p.verified ? <Badge tone="ok">{t('signedOk')}</Badge> : p.signed ? <Badge tone="err">{t('invalidSignature')}</Badge> : <Badge tone="neutral">{t('unsigned')}</Badge>}
            {p.signatureError && <small className="plugins-muted"> {p.signatureError}</small>}
          </dd>
          <dt>{t('kv.commands')}</dt><dd>{p.capabilities.commands.map((c) => c.name).join(', ') || t('kv.noCommands')}</dd>
        </dl>
        <p className="plugins-muted"><Icon name="info" size={14} /> {t('settingsHint')}</p>
      </Dialog>
    </Panel>
  );
}
