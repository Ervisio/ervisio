export interface MenuSize {
  width: number;
  height: number;
}
export interface MenuPoint {
  x: number;
  y: number;
}

/** Fixed-position menus use their measured dimensions, including short taskbar menus. */
export function menuPosition(
  point: MenuPoint,
  size: MenuSize,
  viewport: MenuSize,
  above = false,
): MenuPoint {
  const maxX = Math.max(8, viewport.width - size.width - 8);
  const maxY = Math.max(8, viewport.height - size.height - 8);
  return {
    x: Math.max(8, Math.min(point.x, maxX)),
    y: Math.max(8, Math.min(above ? point.y - size.height - 8 : point.y, maxY)),
  };
}
