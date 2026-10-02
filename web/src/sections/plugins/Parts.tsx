import { useState, type CSSProperties, type ReactNode } from 'react';
import { useT } from '../../i18n';
import { Badge, Icon, type HueId } from '../../ui';
import { describeCaps, whoCanUse } from './caps';
import type { CatalogEntry, PluginInfo } from './types';

export const hueOf = (c?: string): HueId => (['ov', 'term', 'file', 'log', 'svc', 'sw', 'usr', 'plg'].includes(c ?? '') ? (c as HueId) : 'plg');

/** Square icon tile in the plugin's colour. */
export function Tile({ icon, logo, color, off, size = 'md' }: { icon: string; logo?: string; color?: string; off?: boolean; size?: 'sm' | 'md' | 'lg' }) {
  // A logo that does not load (e.g. a disabled plugin's files are not served) falls back to the icon.
  const [failed, setFailed] = useState('');
  const showLogo = !!logo && failed !== logo;
  return (
    <span className={`plugins-tile plugins-tile--${size} hue-${hueOf(color)}${off ? ' is-off' : ''}${showLogo ? ' has-logo' : ''}`}>
      {showLogo ? <img src={logo} alt="" draggable={false} onError={() => setFailed(logo!)} /> : <Icon name={icon || 'plugins'} />}
    </span>
  );
}

/** Verified / Community / Invalid badge. */
export function TrustBadge({ p }: { p: Pick<PluginInfo, 'signed' | 'verified' | 'signatureError'> | { verified: boolean } }) {
  const t = useT('plugins');
  const full = p as PluginInfo;
  if (p.verified) return <Badge tone="ok" dot={false} className="plugins-badge"><Icon name="shield" />{t('verified')}</Badge>;
  if (full.signed) return <Badge tone="err" dot={false} className="plugins-badge"><Icon name="alert" /><span title={full.signatureError}>{t('invalidSignature')}</span></Badge>;
  return <Badge tone="neutral" dot={false} className="plugins-badge">{t('community')}</Badge>;
}

/** The capability list shared by the side panel and the consent dialog. */
export function CapList({ item }: { item: Pick<PluginInfo | CatalogEntry, 'capabilities' | 'contributes'> }) {
  const t = useT('plugins');
  return (
    <div className="plugins-perms">
      {describeCaps(item.capabilities, item.contributes, t).map((c) => (
        <div key={c.key} className={`plugins-perm hue-${c.hue}`}>
          <span className="plugins-perm-ic"><Icon name={c.icon} /></span>
          <div>
            <b>{c.title}</b>
            {c.text && <small>{c.text}</small>}
            {c.codes && (
              <span className="plugins-codes">
                {c.codes.map((x) => <code key={x}>{x}</code>)}
              </span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export function WhoCanUse({ groups }: { groups: string[] }) {
  const t = useT('plugins');
  const w = whoCanUse(groups, t);
  return (
    <div className="plugins-perms">
      <div className="plugins-perm hue-usr">
        <span className="plugins-perm-ic"><Icon name="users" /></span>
        <div>
          <b>{w.title}</b>
          <small>{w.text}</small>
        </div>
      </div>
    </div>
  );
}

export function H4({ children }: { children: ReactNode }) {
  return <p className="plugins-h4">{children}</p>;
}

export const hueStyle = (c?: string): CSSProperties => ({ ['--h' as string]: `var(--h-${hueOf(c)})`, ['--s' as string]: `var(--h-${hueOf(c)}-s)` });
