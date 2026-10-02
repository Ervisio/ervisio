import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'template' } (id). Placeholder: replace the body, keep the signature. */
export function TemplatePage(_props: RouteProps<'template'>) {
  return <ComingSoon icon="store" name={t('nav.templates')} back={true} />;
}
