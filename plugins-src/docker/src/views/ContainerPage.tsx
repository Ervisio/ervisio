import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'container' } (id, optional tab). Placeholder: replace the body, keep the signature. */
export function ContainerPage(_props: RouteProps<'container'>) {
  return <ComingSoon icon="box" name={t('nav.containers')} back={true} />;
}
