// Paints the cached theme before the app boots so there is no flash (kept external for the strict CSP).
try {
  var c = JSON.parse(localStorage.getItem('la.themeVars') || 'null');
  if (c) {
    var r = document.documentElement;
    for (var k in c.vars) r.style.setProperty(k, c.vars[k]);
    r.style.colorScheme = c.scheme;
    r.style.background = c.vars['--bg'];
  }
} catch (e) {}
