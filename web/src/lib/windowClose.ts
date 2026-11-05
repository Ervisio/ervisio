export interface WindowCloseRequest {
  id: string;
  close(): void;
}

/** Lets a document app confirm unsaved edits before the desktop removes its window. */
export function requestWindowClose(id: string, close: () => void): void {
  const event = new CustomEvent<WindowCloseRequest>('ervisio:before-window-close', {
    cancelable: true,
    detail: { id, close },
  });
  if (window.dispatchEvent(event)) close();
}
