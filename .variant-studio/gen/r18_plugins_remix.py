import sys, os, re
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *
import r17_plugins as P

R17 = os.path.join(ROUNDS, '017-plugins')

def parts(v):
    """Variant-specific CSS and the inner section of a round-17 mock."""
    s = open(os.path.join(R17, f'{v}.html')).read()
    css = s[s.index('<style>')+7:s.index('</style>')].replace(SHELL_CSS, '').replace(P.P_CSS, '')
    sec = s[s.index('<section class="fm pg">')+len('<section class="fm pg">'):s.index('</section>')]
    sec = re.sub(r'<div class="pbar">.*?</div><div class="sp"></div>.*?</div>\n', '', sec, count=1, flags=re.S)
    return css, sec

CA, SA = parts('a'); CB, SB = parts('b'); CC, SC = parts('c')
# consent dialog only on demand
SB = SB.replace('<div class="scrim">', '<div class="scrim" hidden>')

def cut(html, start, end_marker):
    i = html.index(start); j = html.index(end_marker, i) + len(end_marker)
    return html[i:j]
SUM = cut(SC, '<div class="sum">', '</div>\n      </div>')
TABLE = cut(SC, '<div class="tb">', '</table></div>')
DEV = cut(SC, '<div class="dev">', '</div></div>')
DETAIL = cut(SA, '<aside class="det">', '</aside>')
GRID = cut(SA, '<div class="grid">', 'Browse plugins</div></div>')

TABS = [('installed','Installed','6'),('updates','Updates','1'),('browse','Browse',''),('security','Security',''),('developer','Developer','')]
def tabbar(names):
    t = ''.join(f'<button class="ftab{" on" if k=="installed" else ""}" data-go="{k}">{n}{" <b>"+c+"</b>" if c else ""}</button>' for k,n,c in TABS if k in names)
    return f'<div class="pbar"><div class="ftabs">{t}</div><div class="sp"></div><div class="fsearch">{I("search")}Search plugins</div><button class="abtn p" data-go="browse">{I("plus")}Get plugins</button></div>'

CSS = P.P_CSS + f'''
.pgm{{flex:1;display:flex;flex-direction:column;min-height:calc(100vh - 90px);position:relative}}
.pane{{display:none;flex:1;min-height:0}}
.pane.on{{display:flex}}
.va_{{ {CA} }}
.vb_{{ {CB} }}
.vc_{{ {CC} }}
.vb_ .pg,.va_ .pg,.vc_ .pg{{min-height:0}}
.scrim[hidden]{{display:none}}
'''
JS = '''<script>
(function(){const root=document.querySelector('.pgm');
function go(k){root.querySelectorAll('.pane').forEach(p=>p.classList.toggle('on',p.dataset.pane===k));
  root.querySelectorAll('.ftab').forEach(t=>t.classList.toggle('on',t.dataset.go===k))}
root.addEventListener('click',e=>{const g=e.target.closest('[data-go]');if(g){go(g.dataset.go);return}
  const b=e.target.closest('button');if(!b)return;const sc=root.querySelector('.scrim');
  if(/^\\s*Install\\s*$/.test(b.textContent)&&sc){sc.hidden=false}
  if(sc&&(b.textContent.trim()==='Cancel'||b.textContent.trim()==='Install and allow'))sc.hidden=true});
})();
</script>'''

def pane(k, cls, inner, layout='row'):
    fl = 'flex-direction:column' if layout == 'col' else ''
    return f'<div class="pane {cls}{" on" if k=="installed" else ""}" data-pane="{k}"><div class="pg" style="display:flex;{fl};flex:1;min-width:0">{inner}</div></div>'

BROWSE = pane('browse', 'vb_', SB, 'col')
UPDATES = pane('updates', 'va_', f'<div class="pm"><div class="grid">{P.pcard(*P.PL[0])}</div></div>{DETAIL}')

def build(cls, installed, extra_panes, names):
    main = top('Search plugins') + f'''
    <section class="fm pgm">
      {tabbar(names)}
      {installed}{UPDATES}{BROWSE}{extra_panes}
    </section>'''
    return page(cls, CSS, main, 'c-plg', JS, P.EXTRA)

# A: one tab per view
A = build('va', pane('installed','va_', f'<div class="pm">{GRID}</div>{DETAIL}'),
          pane('security','vc_', SUM + TABLE, 'col') + pane('developer','vc_', DEV, 'col'),
          ['installed','updates','browse','security','developer'])
# B: risk summary on top of the installed cards, matrix folded into Security tab removed
B = build('vb', pane('installed','va_', f'<div class="pm"><div class="vc_">{SUM}</div>{GRID}</div>{DETAIL}'),
          pane('developer','vc_', DEV, 'col'),
          ['installed','updates','browse','developer'])
# C: installed as the capability table, details panel on the right
C = build('vc', pane('installed','vc_', f'<div class="pm" style="flex:1;min-width:0;display:flex;flex-direction:column">{SUM}{TABLE}{DEV}</div><div class="va_" style="display:flex">{DETAIL}</div>'),
          '', ['installed','updates','browse'])

write('018-plugins-remix', {'a': A, 'b': B, 'c': C})
