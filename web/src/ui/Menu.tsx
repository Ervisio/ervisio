import { cloneElement, isValidElement, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type MouseEvent, type ReactElement, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Icon, type IconName } from './Icon';
import { useOutsideClick } from './hooks';

/* ---------- Tooltip ---------- */
export function Tooltip({ label, children, delay = 350 }: { label: ReactNode; children: ReactElement; delay?: number }) {
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const timer = useRef<number>();
  const id = useId();
  const ref = useRef<HTMLSpanElement>(null);
  const show = () => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      const r = ref.current?.getBoundingClientRect();
      if (r) setPos({ x: Math.min(Math.max(r.left + r.width / 2, 60), window.innerWidth - 60), y: r.top - 8 });
    }, delay);
  };
  const hide = () => {
    window.clearTimeout(timer.current);
    setPos(null);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <span ref={ref} style={{ display: 'inline-flex' }} onMouseEnter={show} onMouseLeave={hide} onFocus={(e) => { if ((e.target as HTMLElement).matches?.(':focus-visible')) show(); }} onBlur={hide} onKeyDown={(e) => e.key === 'Escape' && hide()}>
      {isValidElement(children) ? cloneElement(children as ReactElement<any>, { 'aria-describedby': pos ? id : undefined }) : children}
      {pos && createPortal(<div role="tooltip" id={id} className="ui-tip" style={{ left: pos.x, top: pos.y }}>{label}</div>, document.body)}
    </span>
  );
}

/* ---------- Menu ---------- */
export type MenuItem =
  | { type?: 'item'; id: string; label: string; icon?: IconName; kbd?: string; danger?: boolean; disabled?: boolean; onSelect(): void }
  | { type: 'separator' }
  | { type: 'heading'; label: string };

export type MenuAnchor = { x: number; y: number } | { rect: DOMRect };

export function Menu({ items, anchor, onClose, 'aria-label': aria }: { items: MenuItem[]; anchor: MenuAnchor; onClose(): void; 'aria-label'?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const [active, setActive] = useState(-1);
  const actionable = useMemo(() => items.map((it, i) => ((it.type ?? 'item') === 'item' && !(it as any).disabled ? i : -1)).filter((i) => i >= 0), [items]);

  useOutsideClick(ref, onClose);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    let left: number;
    let top: number;
    if ('rect' in anchor) {
      left = anchor.rect.left;
      top = anchor.rect.bottom + 6;
      if (left + w > window.innerWidth - 8) left = anchor.rect.right - w;
      if (top + h > window.innerHeight - 8) top = anchor.rect.top - h - 6;
    } else {
      left = anchor.x;
      top = anchor.y;
      if (left + w > window.innerWidth - 8) left = anchor.x - w;
      if (top + h > window.innerHeight - 8) top = anchor.y - h;
    }
    setPos({ left: Math.max(8, left), top: Math.max(8, top) });
    el.focus();
  }, [anchor]);
  useEffect(() => {
    const close = () => onClose();
    window.addEventListener('resize', close);
    window.addEventListener('blur', close);
    return () => {
      window.removeEventListener('resize', close);
      window.removeEventListener('blur', close);
    };
  }, [onClose]);

  const move = (dir: 1 | -1) => {
    if (!actionable.length) return;
    const cur = actionable.indexOf(active);
    const n = cur < 0 ? (dir === 1 ? 0 : actionable.length - 1) : (cur + dir + actionable.length) % actionable.length;
    setActive(actionable[n]);
  };
  const choose = (it: MenuItem) => {
    if ((it.type ?? 'item') !== 'item') return;
    onClose();
    (it as Extract<MenuItem, { id: string }>).onSelect();
  };

  return createPortal(
    <div
      ref={ref}
      className="ui-menu"
      role="menu"
      aria-label={aria}
      tabIndex={-1}
      style={pos ? { left: pos.left, top: pos.top } : { left: 0, top: 0, visibility: 'hidden' }}
      onKeyDown={(e) => {
        if (e.key === 'ArrowDown') {
          e.preventDefault();
          move(1);
        } else if (e.key === 'ArrowUp') {
          e.preventDefault();
          move(-1);
        } else if (e.key === 'Home') {
          e.preventDefault();
          setActive(actionable[0] ?? -1);
        } else if (e.key === 'End') {
          e.preventDefault();
          setActive(actionable[actionable.length - 1] ?? -1);
        } else if (e.key === 'Escape') {
          e.stopPropagation();
          onClose();
        } else if (e.key === 'Tab') {
          onClose();
        } else if ((e.key === 'Enter' || e.key === ' ') && active >= 0) {
          e.preventDefault();
          choose(items[active]);
        }
      }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {items.map((it, i) => {
        if (it.type === 'separator') return <div key={i} className="ui-msep" role="separator" />;
        if (it.type === 'heading') return <div key={i} className="ui-mgl">{it.label}</div>;
        return (
          <button
            key={it.id}
            type="button"
            role="menuitem"
            tabIndex={-1}
            disabled={it.disabled}
            data-active={i === active}
            className={`ui-mi${it.danger ? ' ui-mi--danger' : ''}`}
            onMouseEnter={() => setActive(i)}
            onClick={() => choose(it)}
          >
            {it.icon && <Icon name={it.icon} />}
            {it.label}
            {it.kbd && <kbd className="ui-kbd">{it.kbd}</kbd>}
          </button>
        );
      })}
    </div>,
    document.body,
  );
}

/** Dropdown opened from a trigger. `trigger` receives props to spread on your button. */
export function DropdownMenu({ items, trigger, 'aria-label': aria }: { items: MenuItem[]; trigger(p: { onClick(e: MouseEvent<HTMLElement>): void; 'aria-haspopup': 'menu'; 'aria-expanded': boolean }): ReactNode; 'aria-label'?: string }) {
  const [anchor, setAnchor] = useState<MenuAnchor | null>(null);
  const close = useCallback(() => setAnchor(null), []);
  return (
    <>
      {trigger({
        onClick: (e) => setAnchor((a) => (a ? null : { rect: e.currentTarget.getBoundingClientRect() })),
        'aria-haspopup': 'menu',
        'aria-expanded': !!anchor,
      })}
      {anchor && <Menu items={items} anchor={anchor} onClose={close} aria-label={aria} />}
    </>
  );
}

/** Right-click menu. Spread `bind` on the element and render `menu` anywhere. */
export function useContextMenu(getItems: () => MenuItem[]) {
  const [state, setState] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null);
  const close = useCallback(() => setState(null), []);
  const bind = {
    onContextMenu: (e: MouseEvent) => {
      e.preventDefault();
      setState({ x: e.clientX, y: e.clientY, items: getItems() });
    },
  };
  const menu = state ? <Menu items={state.items} anchor={{ x: state.x, y: state.y }} onClose={close} /> : null;
  return { bind, menu, open: (x: number, y: number) => setState({ x, y, items: getItems() }), close };
}
