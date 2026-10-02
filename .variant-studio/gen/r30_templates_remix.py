import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *
import r29_templates as T

src = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'r29_templates.py')).read()
A_CSS = src.split("'va', '''")[1].split("''')")[0]
B_CSS = src.split("'vb', '''")[1].split("''')")[0]
GRID = f'''<div class="bar1"><div class="in">{I('search')}<input placeholder="Search 140 apps"></div></div>
  <div class="cats">{''.join(f'<button class="cat{" on" if i==0 else ""}">{c}</button>' for i,c in enumerate(T.CATS))}</div>
  <div class="tgrid">{''.join(T.card(t).replace('<div class="tc ', '<div data-app class="tc ', 1) for t in T.T)}</div>'''
JS = '''<script>
document.querySelectorAll('[data-app]').forEach(c=>c.addEventListener('click',()=>{document.querySelector('.q').classList.add('open')}));
document.querySelectorAll('[data-close]').forEach(c=>c.addEventListener('click',e=>{e.stopPropagation();document.querySelector('.q').classList.remove('open')}));
</script>'''
DET = T.DETAIL.replace('<a class="b g">'+I('link')+'Website</a>', '<a class="b g">'+I('link')+'Website</a><button class="b ic g" data-close title="Close">'+I('close')+'</button>')

# A: grid + side panel (grid narrows when open)
A = T.wrap(f'''<div class="lay">{'<div class="gwrap">'+GRID+'</div>'}<div class="pnl">{DET}</div></div>''', 'va open',
  A_CSS + B_CSS + '''
.lay{display:grid;grid-template-columns:1fr;gap:12px;align-items:start}
.q.open .lay{grid-template-columns:1fr 440px}
.gwrap{display:flex;flex-direction:column;gap:12px;min-width:0}
.pnl{display:none;position:sticky;top:12px}.q.open .pnl{display:block}
.q.open .tgrid{grid-template-columns:repeat(auto-fill,minmax(230px,1fr))}
.tc{cursor:pointer;outline:2px solid transparent}.tc:hover{outline-color:var(--line)}
.det .g2{grid-template-columns:1fr}
@media(max-width:1200px){.q.open .lay{grid-template-columns:1fr}}''') + JS

# B: grid; clicking opens a full app page (store-like detail) with the form
APP = f'''<div class="app">{DET.replace('class="card det h-term"','class="card det h-term wide"')}
  <div class="card shots"><h3>About this app</h3><p class="muted" style="margin:0">Vaultwarden is an unofficial Bitwarden server written in Rust. It stores your vault encrypted; the server never sees your master password.</p>
  <dl class="kv2"><dt>Image</dt><dd class="mono">vaultwarden/server:1.34.3</dd><dt>Maintainer</dt><dd>dani-garcia</dd><dt>License</dt><dd>AGPL-3.0</dd><dt>Docs</dt><dd><a style="color:var(--acc)">github.com/dani-garcia/vaultwarden</a></dd></dl></div></div>'''
B = T.wrap(f'''<div class="storev">{GRID}</div><div class="appv">{APP}</div>''', 'vb open', A_CSS + B_CSS + '''
.storev{display:flex;flex-direction:column;gap:12px}.appv{display:none}
.q.open .storev{display:none}.q.open .appv{display:block}
.app{display:grid;grid-template-columns:1.4fr 1fr;gap:12px;align-items:start}
.tc{cursor:pointer}
@media(max-width:1200px){.app{grid-template-columns:1fr}}''').replace('data-close title="Close">'+I('close'), 'data-close title="Back to all apps">'+I('chevron',' style="transform:rotate(90deg)"')) + JS

if __name__ == '__main__':
    write('030-docker-templates-remix', {'a': A, 'b': B})
