import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'create' } (optional from: container to recreate, image). Placeholder: replace the body, keep the signature. */
export function CreatePage(_props: RouteProps<'create'>) {
  return <ComingSoon icon="box" name={t('containers.new')} back={true} />;
}
