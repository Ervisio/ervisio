/** Desktop consumes the launch; Classic falls back to its ordinary route navigation. */
export function openApp(to: string, navigate: (to: string) => void): void {
  const event = new CustomEvent('ervisio:shell-action', {
    cancelable: true,
    detail: { action: 'open-app', to },
  });
  if (window.dispatchEvent(event)) navigate(to);
}
