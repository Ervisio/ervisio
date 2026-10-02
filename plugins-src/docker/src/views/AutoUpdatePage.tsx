import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'autoupdate' }. Placeholder: replace the body, keep the signature. */
export function AutoUpdatePage(_props: RouteProps<'autoupdate'>) {
  return <ComingSoon icon="refresh" name={t('nav.autoupdate')} back={false} />;
}
