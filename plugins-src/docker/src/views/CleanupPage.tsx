import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'cleanup' }. Placeholder: replace the body, keep the signature. */
export function CleanupPage(_props: RouteProps<'cleanup'>) {
  return <ComingSoon icon="broom" name={t('nav.cleanup')} back={false} />;
}
