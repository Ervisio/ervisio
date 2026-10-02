import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'volumes' }. Placeholder: replace the body, keep the signature. */
export function VolumesPage(_props: RouteProps<'volumes'>) {
  return <ComingSoon icon="database" name={t('nav.volumes')} back={false} />;
}
