# Shared helpers for the mock generators: app shell (rail, top bar, dock), icons, fonts.
import os
HERE = os.path.dirname(os.path.abspath(__file__))
ROUNDS = os.path.join(os.path.dirname(HERE), 'rounds')

ARCH = '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M11.39.605C10.376 3.092 9.764 4.72 8.635 7.132c.693.734 1.543 1.589 2.923 2.554-1.484-.61-2.496-1.224-3.252-1.86C6.86 10.842 4.596 15.138 0 23.395c3.612-2.085 6.412-3.37 9.021-3.862a6.61 6.61 0 0 1-.171-1.547l.003-.115c.058-2.315 1.261-4.095 2.687-3.973 1.426.12 2.534 2.096 2.478 4.409a6.52 6.52 0 0 1-.146 1.243c2.58.505 5.352 1.787 8.914 3.844-.702-1.293-1.33-2.459-1.929-3.57-.943-.73-1.926-1.682-3.933-2.713 1.38.359 2.367.772 3.137 1.234-6.09-11.334-6.582-12.84-8.67-17.74z"/></svg>'
FONT = '<link rel="preconnect" href="https://fonts.googleapis.com"><link href="https://fonts.googleapis.com/css2?family=Figtree:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">'
SHELL_CSS = open(os.path.join(HERE, 'shell.css')).read()

def I(n, s=''): return f'<svg class="i"{s}><use href="#i-{n}"/></svg>'

NAV = [('c-ov','overview','Overview',''),('c-term','terminal','Terminal',''),('c-file','files','Files',''),
       ('c-log','logs','Logs',''),('c-svc','services','Services','1'),('c-sw','software','Software','23'),
       ('c-usr','users','Users',''),('c-plg','plugins','Plugins','')]

def rail(active, extra=()):
    items = ''
    for c, ic, lb, n in NAV:
        if c == 'c-plg': items += '<div class="rsep"></div>'
        b = f'<span class="n">{n}</span>' if n else ''
        items += f'<a class="it {c}{" on" if c==active else ""}" href="#"><span class="pi">{I(ic)}</span><span class="lb">{lb}</span>{b}</a>'
    for c, ic, lb in extra:
        items += f'<a class="it {c}" href="#"><span class="pi">{I(ic)}</span><span class="lb">{lb}</span></a>'
    return f'''<nav class="rail">
    <a class="distro" href="#"><span class="lg">{ARCH}</span><span class="dn">arch</span></a>
    {items}<div class="sp"></div><a class="it c-set{" on" if active=="c-set" else ""}" href="#"><span class="pi">{I('cog')}</span><span class="lb">Settings</span></a><div class="me"><div class="av">F</div></div></nav>'''

def top(ph):
    return f'''<div class="top">
      <div class="search">{I('search')}{ph}</div><div class="sp"></div>
      <div class="pill"><i></i>arch{I('chevron',' style="width:16px;height:16px"')}</div>
      <button class="ib" data-toggle-theme title="Theme">{I('moon')}</button>
      <button class="ib" title="Notifications">{I('bell')}</button></div>'''

def dock(active):
    items = [('c-ov','overview','Home'),('c-term','terminal','Terminal'),('c-file','files','Files'),('c-svc','services','Services')]
    out = ''.join(f'<a class="{c}{" on" if c==active else ""}" href="#"><span class="pi">{I(ic)}</span>{l}</a>' for c,ic,l in items)
    return f'<nav class="dock">{out}<a href="#"><span class="pi">{I("more")}</span>More</a></nav>'

def page(cls, css, main, active, js='', extra=()):
    return f'''{FONT}
<style>{SHELL_CSS}{css}</style>
<div class="q {cls}">
  {rail(active, extra)}
  <main class="main">
    {main}
  </main>
  {dock(active)}
</div>
<script src="kit.js"></script>{js}'''

def write(round_id, variants):
    d = os.path.join(ROUNDS, round_id)
    for k, v in variants.items():
        open(os.path.join(d, f'{k}.html'), 'w').write(v)
    print('ok', round_id)
