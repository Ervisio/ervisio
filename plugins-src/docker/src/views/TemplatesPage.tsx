import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'templates' }. Placeholder: replace the body, keep the signature. */
export function TemplatesPage(_props: RouteProps<'templates'>) {
  return <ComingSoon icon="store" name={t('nav.templates')} back={false} />;
}
