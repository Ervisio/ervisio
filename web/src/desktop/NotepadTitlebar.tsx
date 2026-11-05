import { useContext, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { WindowTitlebarSlot } from './AppWindow';
import './notepad-titlebar.css';

/** The classic page retains its tabs; ErvisiOS uses the window's shared title bar. */
export function NotepadTitlebar({ children }: { children: ReactNode }) {
  const slot = useContext(WindowTitlebarSlot);
  return slot
    ? createPortal(<div className="notepad-window-tabs">{children}</div>, slot)
    : children;
}
