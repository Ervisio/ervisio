import type { CSSProperties, SVGAttributes } from 'react';
import { ICON_PATHS } from './iconPaths';

export type IconName = keyof typeof ICON_PATHS | (string & {});

/** Sections may add icons (e.g. a plugin) with registerIcon('name', '<path d="..."/>'). */
export function registerIcon(name: string, innerSvg: string) {
  ICON_PATHS[name] = innerSvg;
}

export interface IconProps extends Omit<SVGAttributes<SVGSVGElement>, 'name'> {
  name: IconName;
  size?: number;
  style?: CSSProperties;
}

export function Icon({ name, size, className, style, ...rest }: IconProps) {
  const inner = ICON_PATHS[name] ?? ICON_PATHS.info;
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.7}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      className={`ui-icon${className ? ' ' + className : ''}`}
      style={size ? { width: size, height: size, ...style } : style}
      dangerouslySetInnerHTML={{ __html: inner }}
      {...rest}
    />
  );
}
