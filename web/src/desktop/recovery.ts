type RecoveryApp = { key: string; to: string };
type RecoveryIntent = { scope: string; app: string; url: string; created: number };
const KEY = 'ervisio.desktop-recovery';
const TTL = 5 * 60_000;

function validURL(app: RecoveryApp, value: unknown): value is string {
  if (
    typeof value !== 'string' ||
    value.length > 16_384 ||
    !value.startsWith('/') ||
    value.startsWith('//') ||
    [...value].some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127)
  )
    return false;
  try {
    const base = new URL(app.to, 'https://ervisio.invalid').pathname;
    const target = new URL(value, 'https://ervisio.invalid');
    return (
      target.origin === 'https://ervisio.invalid' &&
      (target.pathname === base || (base !== '/' && target.pathname.startsWith(`${base}/`)))
    );
  } catch {
    return false;
  }
}

/** Only the explicit chunk-recovery action records an app. Ordinary reloads stay on the desktop. */
export function rememberDesktopRecovery(
  storage: Pick<Storage, 'setItem'>,
  scope: string,
  app: RecoveryApp,
  url: string,
  now = Date.now(),
): void {
  if (!scope || !validURL(app, url)) return;
  try {
    storage.setItem(
      KEY,
      JSON.stringify({ scope, app: app.key, url, created: now } satisfies RecoveryIntent),
    );
  } catch {
    /* A blocked browser store still permits a full interface reload. */
  }
}

export function takeDesktopRecovery(
  storage: Pick<Storage, 'getItem' | 'removeItem'>,
  scope: string,
  apps: RecoveryApp[],
  now = Date.now(),
): RecoveryIntent | null {
  try {
    const raw = storage.getItem(KEY);
    storage.removeItem(KEY);
    if (!raw || raw.length > 20_000) return null;
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== 'object') return null;
    const intent = value as Partial<RecoveryIntent>;
    const app = apps.find((item) => item.key === intent.app);
    if (
      !app ||
      intent.scope !== scope ||
      typeof intent.created !== 'number' ||
      !Number.isFinite(intent.created) ||
      intent.created > now ||
      now - intent.created > TTL ||
      !validURL(app, intent.url)
    )
      return null;
    return intent as RecoveryIntent;
  } catch {
    return null;
  }
}
