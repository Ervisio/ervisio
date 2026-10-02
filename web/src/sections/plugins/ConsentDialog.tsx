import { useT } from '../../i18n';
import { Button, Dialog } from '../../ui';
import { CapList, Tile, TrustBadge } from './Parts';
import { whoCanUse } from './caps';
import type { CatalogEntry } from './types';

/** "Install X?" with exactly the capabilities the package declares. */
export function ConsentDialog({ entry, mode, busy, onCancel, onConfirm }: { entry: CatalogEntry | null; mode: 'install' | 'update'; busy: boolean; onCancel(): void; onConfirm(): void }) {
  const t = useT('plugins');
  if (!entry) return null;
  const who = whoCanUse(entry.visibleTo.groups, t);
  return (
    <Dialog
      open
      onClose={onCancel}
      title={t(mode === 'install' ? 'consent.titleInstall' : 'consent.titleUpdate', { name: entry.name })}
      description={
        <span className="plugins-consent-sub">
          <Tile icon={entry.icon} logo={entry.logo} color={entry.color} size="sm" />
          <span>{entry.author}, v{entry.version}</span>
          <TrustBadge p={{ verified: entry.verified }} />
        </span>
      }
      footer={
        <>
          <Button onClick={onCancel}>{t('cancel')}</Button>
          <Button variant="primary" loading={busy} onClick={onConfirm}>{t(mode === 'install' ? 'consent.allow' : 'consent.allowUpdate')}</Button>
        </>
      }
    >
      <p className="plugins-lead">{entry.verified ? t('consent.lead') : t('consent.leadCommunity')}</p>
      <CapList item={entry} />
      <p className="plugins-muted">{who.title}. {who.text}</p>
      <p className="plugins-muted">{t('consent.sandbox')}</p>
    </Dialog>
  );
}
