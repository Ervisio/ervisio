import { EmptyState } from '../ui';
import { useT } from '../i18n';
import type { HueId } from '../ui';

/** Shown by sections nobody has built yet. Section agents replace their folder's index.tsx. */
export default function Placeholder({ icon, hue }: { icon: string; hue: HueId }) {
  const t = useT('shell');
  return (
    <div style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
      <EmptyState icon={icon} hue={hue} title={t('placeholder.title')} text={t('placeholder.text')} />
    </div>
  );
}
