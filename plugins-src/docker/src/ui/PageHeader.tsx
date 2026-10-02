import type { ReactNode } from 'react';
import { back, useCanGoBack } from '../router';
import { t } from '../i18n';
import { Icon, IconButton, type HueId } from '../kit';

/**
 * Title row of a view: icon tile, title, subtitle, actions on the right. Pass `back` on detail views
 * (container, stack, template, create) to show a Back button that uses the router history.
 */
export function PageHeader({ icon, hue = 'file', title, subtitle, actions, back: showBack }: {
  icon: string;
  hue?: HueId;
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  back?: boolean;
}) {
  const canBack = useCanGoBack();
  return (
    <header className={`dk-ph hue-${hue}`}>
      {showBack && canBack && <IconButton icon="chevronleft" label={t('common.back')} onClick={back} />}
      <span className="dk-ph-ic"><Icon name={icon} /></span>
      <div className="dk-ph-tx">
        <h1>{title}</h1>
        {subtitle && <p>{subtitle}</p>}
      </div>
      {actions && <div className="dk-ph-act">{actions}</div>}
    </header>
  );
}
