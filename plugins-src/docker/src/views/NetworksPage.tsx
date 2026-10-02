import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'networks' }. Placeholder: replace the body, keep the signature. */
export function NetworksPage(_props: RouteProps<'networks'>) {
  return <ComingSoon icon="net" name={t('nav.networks')} back={false} />;
}
