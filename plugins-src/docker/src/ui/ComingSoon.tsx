import { t } from '../i18n';
import { EmptyState } from '../kit';
import { PageHeader } from './PageHeader';

/** Body of the views nobody has built yet. Replace it when you fill a view. */
export function ComingSoon({ icon, name, back }: { icon: string; name: string; back?: boolean }) {
  return (
    <>
      <PageHeader icon={icon} title={name} back={back} />
      <EmptyState icon={icon} title={t('soon.title', { name })} text={t('soon.text')} />
    </>
  );
}
