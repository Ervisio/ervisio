// Paints the cached theme before the app boots so there is no flash (kept external for the strict CSP).
// 'la.themeVars' is the key of LinuxAdmin (Ervisio's former name), until src/lib/storageMigration.ts moves it.
try {
  var c = JSON.parse(localStorage.getItem('ervisio.themeVars') || localStorage.getItem('la.themeVars') || 'null');
  if (c) {
    var r = document.documentElement;
    for (var k in c.vars) r.style.setProperty(k, c.vars[k]);
    r.style.colorScheme = c.scheme;
    r.style.background = c.vars['--bg'];
  }
} catch (e) {}
