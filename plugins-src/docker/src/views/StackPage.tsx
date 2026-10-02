import { t } from '../i18n';
import type { RouteProps } from '../router';
import { ComingSoon } from '../ui/ComingSoon';

/** Route { view: 'stack' } (name). Placeholder: replace the body, keep the signature. */
export function StackPage(_props: RouteProps<'stack'>) {
  return <ComingSoon icon="layers" name={t('nav.stacks')} back={true} />;
}
