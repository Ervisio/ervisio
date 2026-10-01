import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Icon, type IconName } from './Icon';
import { Tooltip } from './Menu';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-solid';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  icon?: IconName;
  /** Square icon button. Provide aria-label. */
  iconOnly?: boolean;
  loading?: boolean;
  size?: 'sm' | 'md' | 'lg';
  block?: boolean;
  children?: ReactNode;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', icon, iconOnly, loading, size = 'md', block, className, children, disabled, type = 'button', ...rest },
  ref,
) {
  const cls = ['ui-btn', variant !== 'secondary' && `ui-btn--${variant}`, iconOnly && 'ui-btn--icon', size !== 'md' && `ui-btn--${size}`, block && 'ui-btn--block', className]
    .filter(Boolean)
    .join(' ');
  return (
    <button ref={ref} type={type} className={cls} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
      {loading ? <span className="ui-spin" aria-hidden="true" /> : icon ? <Icon name={icon} /> : null}
      {!iconOnly && children}
    </button>
  );
});

export interface IconButtonProps extends Omit<ButtonProps, 'iconOnly' | 'children' | 'icon'> {
  icon: IconName;
  /** Accessible name, also shown as tooltip. */
  label: string;
  tooltip?: boolean;
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { icon, label, tooltip = true, variant = 'ghost', ...rest },
  ref,
) {
  const b = <Button ref={ref} iconOnly icon={icon} variant={variant} aria-label={label} {...rest} />;
  return tooltip ? <Tooltip label={label}>{b}</Tooltip> : b;
});
