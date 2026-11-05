/** Only local Ervisio UI caches: never browser cookies, authentication, mock data or other apps. */
export function resetBrowserPreferences(storage: Pick<Storage, 'length' | 'key' | 'removeItem'>) {
  const exact = new Set([
    'ervisio.prefs',
    'ervisio.lang',
    'ervisio.themeVars',
    'ervisio.term.layout',
    'ervisio.plugins.movedDismissed',
    'ervisio.recentUsers',
    'ervisio.rememberUsers',
  ]);
  const keys: string[] = [];
  for (let index = 0; index < storage.length; index++) {
    const key = storage.key(index);
    if (
      key &&
      (exact.has(key) || key.startsWith('ervisio.prefs:') || key.startsWith('ervisio.login-shell:'))
    )
      keys.push(key);
  }
  for (const key of keys) storage.removeItem(key);
  return keys.length;
}
