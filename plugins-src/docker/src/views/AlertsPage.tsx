import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'alerts' }. Placeholder: replace the body, keep the signature. */
export function AlertsPage(_props: RouteProps<'alerts'>) {
  return <ComingSoon icon="bell" name={t('nav.alerts')} back={false} />;
}
