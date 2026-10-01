import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useOutsideClick } from '../ui/hooks';

/** Anchored panel for rich popups (avatar, notifications, host switcher). */
export function Popover({ anchor, onClose, children, placement = 'below', label }: { anchor: DOMRect; onClose(): void; children: ReactNode; placement?: 'below' | 'right-up'; label?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  useOutsideClick(ref, onClose);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    let left: number;
    let top: number;
    if (placement === 'right-up') {
      left = anchor.right + 10;
      top = anchor.bottom - h;
    } else {
      left = anchor.right - w;
      top = anchor.bottom + 8;
    }
    left = Math.min(Math.max(8, left), window.innerWidth - w - 8);
    top = Math.min(Math.max(8, top), window.innerHeight - h - 8);
    setPos({ left, top });
    el.focus();
  }, [anchor, placement]);
  return createPortal(
    <div
      ref={ref}
      className="pop"
      role="dialog"
      aria-label={label}
      tabIndex={-1}
      style={pos ?? { left: 0, top: 0, visibility: 'hidden' }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      {children}
    </div>,
    document.body,
  );
}
