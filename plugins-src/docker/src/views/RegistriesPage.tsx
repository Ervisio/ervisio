import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'registries' }. Placeholder: replace the body, keep the signature. */
export function RegistriesPage(_props: RouteProps<'registries'>) {
  return <ComingSoon icon="key" name={t('nav.registries')} back={false} />;
}
