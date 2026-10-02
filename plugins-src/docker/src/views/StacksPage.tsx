import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'stacks' }. Placeholder: replace the body, keep the signature. */
export function StacksPage(_props: RouteProps<'stacks'>) {
  return <ComingSoon icon="layers" name={t('nav.stacks')} back={false} />;
}
