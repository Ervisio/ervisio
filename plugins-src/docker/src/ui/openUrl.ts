import { toast } from '../kit';
import { getSdk } from '../sdk';
import { t } from '../i18n';

/**
 * Opens a URL in a new browser tab. The plugin frame is sandboxed without pop-ups, so this first tries
 * sdk.openExternal when the platform provides it, then window.open, and tells the user the address if both fail.
 */
export function openUrl(url: string): void {
  const sdk = getSdk() as unknown as { openExternal?: (u: string) => void };
  if (typeof sdk.openExternal === 'function') {
    sdk.openExternal(url);
    return;
  }
  const w = window.open(url, '_blank', 'noopener,noreferrer');
  if (!w) toast.info(t('common.openedBlocked', { url }));
}
