import { useT } from '../../i18n';
import { Badge, Switch } from '../../ui';
import { Tile, TrustBadge } from './Parts';
import type { PluginInfo } from './types';

/** Installed plugin card with the on/off switch. */
export function PluginCard({ p, active, onOpen, onToggle, busy }: { p: PluginInfo; active: boolean; onOpen(): void; onToggle(on: boolean): void; busy: boolean }) {
  const t = useT('plugins');
  return (
    <div
      className={`plugins-card${active ? ' is-on' : ''}${p.enabled ? '' : ' is-off'}`}
      role="button"
      tabIndex={0}
      aria-pressed={active}
      onClick={onOpen}
      onKeyDown={(e) => {
        if ((e.key === 'Enter' || e.key === ' ') && e.target === e.currentTarget) {
          e.preventDefault();
          onOpen();
        }
      }}
    >
      <div className="plugins-card-top">
        <Tile icon={p.icon} color={p.color} off={!p.enabled} />
        <div className="plugins-card-tx">
          <b>{p.name}</b>
          <small>{p.author ? `${p.author}, ` : ''}v{p.version}</small>
        </div>
        <span onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
          <Switch checked={p.enabled} disabled={busy || !!p.blocked || !!p.error} onChange={onToggle} aria-label={t('toggle', { name: p.name })} />
        </span>
      </div>
      <p>{p.error ? p.error : p.description || t('noDescription')}</p>
      <div className="plugins-card-meta">
        {p.error ? <Badge tone="err">{t('broken')}</Badge> : <TrustBadge p={p} />}
        {p.updateAvailable && <Badge tone="info" className="plugins-badge">{t('updateBadge', { version: p.updateAvailable.version })}</Badge>}
        {p.blocked && <Badge tone="warn">{t('blocked')}</Badge>}
        {p.location === 'dev' && <Badge tone="neutral">{t('devBadge')}</Badge>}
      </div>
    </div>
  );
}
