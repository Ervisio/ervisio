import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'images' }. Placeholder: replace the body, keep the signature. */
export function ImagesPage(_props: RouteProps<'images'>) {
  return <ComingSoon icon="image" name={t('nav.images')} back={false} />;
}
