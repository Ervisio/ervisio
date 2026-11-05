import { useEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react';
import { Icon } from '../ui';
import { useT } from '../i18n';
import { clampBounds, type WindowBounds } from './model';

const DRAG_THRESHOLD = 6;
const SNAP_EDGE = 12;

export interface DesktopWindow {
  id: string;
  app: string;
  url: string;
  bounds: WindowBounds;
  minimized: boolean;
  maximized: boolean;
  snap?: 'left' | 'right';
}

export function AppWindow({
  win,
  title,
  icon,
  active,
  z,
  onFocus,
  onClose,
  onChange,
  onSaveBounds,
  children,
}: {
  win: DesktopWindow;
  title: string;
  icon: ReactNode;
  active: boolean;
  z: number;
  onFocus(): void;
  onClose(): void;
  onChange(patch: Partial<DesktopWindow>): void;
  onSaveBounds(bounds: WindowBounds): void;
  children: ReactNode;
}) {
  const t = useT('shell');
  const el = useRef<HTMLElement>(null);
  const [dragging, setDragging] = useState(false);
  const preview = useRef(win.bounds);
  const drag = useRef<{
    pointerId: number;
    x: number;
    y: number;
    bounds: WindowBounds;
    resize: boolean;
    restore: boolean;
    moved: boolean;
    anchorX: number;
    anchorY: number;
  } | null>(null);
  const latest = useRef({ win, onChange, onSaveBounds });
  latest.current = { win, onChange, onSaveBounds };

  useEffect(() => {
    const resize = () => {
      const node = el.current?.parentElement;
      if (node)
        latest.current.onChange({
          bounds: clampBounds(latest.current.win.bounds, node.clientWidth, node.clientHeight),
        });
    };
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);

  const begin = (e: PointerEvent<HTMLElement>, resize: boolean) => {
    if (drag.current) return;
    if (e.button !== 0 || (!resize && (e.target as HTMLElement).closest('button'))) return;
    if (resize && (win.maximized || win.snap)) return;
    const rectangle = el.current?.getBoundingClientRect();
    if (!rectangle) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    onFocus();
    drag.current = {
      pointerId: e.pointerId,
      x: e.clientX,
      y: e.clientY,
      bounds: win.bounds,
      resize,
      restore: win.maximized || !!win.snap,
      moved: false,
      anchorX: Math.max(0, Math.min(1, (e.clientX - rectangle.left) / rectangle.width)),
      anchorY: Math.max(0, e.clientY - e.currentTarget.getBoundingClientRect().top),
    };
    preview.current = win.bounds;
  };
  const move = (e: PointerEvent<HTMLElement>) => {
    const d = drag.current,
      host = el.current?.parentElement;
    if (!d || d.pointerId !== e.pointerId || !host) return;
    const dx = e.clientX - d.x,
      dy = e.clientY - d.y;
    if (!d.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD) return;
    d.moved = true;
    setDragging(true);
    if (d.restore) {
      const rectangle = host.getBoundingClientRect();
      const bounds = clampBounds(
        {
          ...d.bounds,
          x: e.clientX - rectangle.left - d.bounds.width * d.anchorX,
          y: e.clientY - rectangle.top - d.anchorY,
        },
        host.clientWidth,
        host.clientHeight,
      );
      // Restore the retained normal size below this pointer, then continue
      // using the same captured gesture and titlebar DOM node.
      d.restore = false;
      d.x = e.clientX;
      d.y = e.clientY;
      d.bounds = bounds;
      preview.current = bounds;
      onChange({ bounds, maximized: false, snap: undefined });
      return;
    }
    const b = d.resize
      ? {
          ...d.bounds,
          width: Math.max(360, d.bounds.width + dx),
          height: Math.max(260, d.bounds.height + dy),
        }
      : { ...d.bounds, x: d.bounds.x + dx, y: d.bounds.y + dy };
    preview.current = clampBounds(b, host.clientWidth, host.clientHeight);
    onChange({ bounds: preview.current });
  };
  const end = (e: PointerEvent<HTMLElement>, snap = true) => {
    const gesture = drag.current;
    if (gesture && gesture.pointerId === e.pointerId) {
      drag.current = null;
      const host = el.current?.parentElement;
      if (gesture.moved) {
        if (snap && !gesture.resize && host && host.clientWidth >= 860) {
          const rectangle = host.getBoundingClientRect();
          if (e.clientY <= rectangle.top + SNAP_EDGE) {
            onChange({ maximized: true, snap: undefined });
          } else if (e.clientX <= rectangle.left + SNAP_EDGE) {
            onChange({ maximized: false, snap: 'left' });
          } else if (e.clientX >= rectangle.right - SNAP_EDGE) {
            onChange({ maximized: false, snap: 'right' });
          }
        }
        onSaveBounds(preview.current);
      }
      setDragging(false);
      if (e.currentTarget.hasPointerCapture(e.pointerId))
        e.currentTarget.releasePointerCapture(e.pointerId);
    }
  };
  return (
    <section
      ref={el}
      className={`eos-window${active ? ' is-active' : ''}${win.maximized ? ' is-maximized' : ''}${win.snap ? ` is-snapped-${win.snap}` : ''}${dragging ? ' is-dragging' : ''}`}
      style={{
        left: win.bounds.x,
        top: win.bounds.y,
        width: win.bounds.width,
        height: win.bounds.height,
        zIndex: z,
        display: win.minimized ? 'none' : undefined,
      }}
      role="region"
      aria-label={title}
      onPointerDownCapture={onFocus}
    >
      <header
        className="eos-titlebar"
        onPointerDown={(e) => begin(e, false)}
        onPointerMove={move}
        onPointerUp={(e) => end(e)}
        onPointerCancel={(e) => end(e, false)}
        onLostPointerCapture={(e) => end(e, false)}
        onDoubleClick={(e) =>
          !(e.target as HTMLElement).closest('button') &&
          onChange({ maximized: !win.maximized, snap: undefined })
        }
      >
        <span className="eos-title-icon">{icon}</span>
        <span className="eos-title">{title}</span>
        <div className="eos-window-controls">
          <button
            title={t('desktop.snapLeft')}
            aria-label={t('desktop.snapLeft')}
            onClick={() =>
              onChange({ snap: win.snap === 'left' ? undefined : 'left', maximized: false })
            }
          >
            <Icon name="split" />
          </button>
          <button
            title={t('desktop.minimize')}
            aria-label={t('desktop.minimize')}
            onClick={() => onChange({ minimized: true })}
          >
            <span className="eos-minimize-icon" />
          </button>
          <button
            title={t(win.maximized ? 'desktop.restore' : 'desktop.maximize')}
            aria-label={t(win.maximized ? 'desktop.restore' : 'desktop.maximize')}
            onClick={() => onChange({ maximized: !win.maximized, snap: undefined })}
          >
            <Icon name={win.maximized ? 'copy' : 'stop'} />
          </button>
          <button
            className="eos-close"
            title={t('desktop.close')}
            aria-label={t('desktop.close')}
            onClick={onClose}
          >
            <Icon name="close" />
          </button>
        </div>
      </header>
      <div className="eos-window-body">{children}</div>
      {!win.maximized && !win.snap && (
        <div
          className="eos-resize"
          role="separator"
          aria-label={t('desktop.resize')}
          aria-orientation="horizontal"
          tabIndex={0}
          onPointerDown={(e) => begin(e, true)}
          onPointerMove={move}
          onPointerUp={(e) => end(e)}
          onPointerCancel={(e) => end(e, false)}
          onLostPointerCapture={(e) => end(e, false)}
          onKeyDown={(e) => {
            const delta = e.shiftKey ? 50 : 10;
            const b = { ...win.bounds };
            if (e.key === 'ArrowRight') b.width += delta;
            else if (e.key === 'ArrowLeft') b.width -= delta;
            else if (e.key === 'ArrowDown') b.height += delta;
            else if (e.key === 'ArrowUp') b.height -= delta;
            else return;
            e.preventDefault();
            const host = el.current?.parentElement;
            if (host) {
              const bounds = clampBounds(b, host.clientWidth, host.clientHeight);
              onChange({ bounds });
              onSaveBounds(bounds);
            }
          }}
        />
      )}
    </section>
  );
}
