import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *
import r19_settings as R

# Section list on the left (A) that scrolls one long searchable page (B). No hairline borders anywhere.
NOLINES = '''
.set{border-top:0!important}
.hostr{border-top:0!important}
.grp{border:0!important}
'''
LAYOUT = '''
.st{flex:1;display:flex;min-height:calc(100vh - 90px);max-height:calc(100vh - 90px)}
.snav{width:260px;flex:none;padding:16px 12px;display:flex;flex-direction:column;gap:2px;overflow:auto;background:var(--sunk);border-radius:22px 0 0 22px}
.snav h2{margin:0 10px 12px;font-size:20px;font-weight:800}
.snav .gl{font-size:12.5px;color:var(--ink3);font-weight:700;margin:14px 10px 4px}
.ni{display:flex;align-items:center;gap:10px;padding:6px 10px 6px 6px;border-radius:14px;color:var(--ink);text-decoration:none;font-weight:600}
.ni:hover{background:var(--surface)}
.ni.on{background:var(--surface);box-shadow:0 1px 3px rgba(0,0,0,.08)}
.ni .ft-ic{width:30px;height:30px;border-radius:10px}.ni .ft-ic .i{width:15px;height:15px}
.ni > .i{width:13px;height:13px;margin-left:auto;color:var(--ink3)}
.scroll{flex:1;min-width:0;padding:22px 32px 80px;overflow:auto;scroll-behavior:smooth}
.inner{max-width:780px}
.hs{display:flex;align-items:center;gap:12px;height:50px;padding:0 20px;border-radius:999px;background:var(--sunk);color:var(--ink3);font-size:15px;margin-bottom:22px}
.hs input{flex:1;border:0;background:none;outline:0;font:inherit;color:var(--ink)}
.hs .i{width:20px;height:20px}
.part{font-size:13px;font-weight:800;color:var(--ink3);margin:36px 0 6px}
.part:first-of-type{margin-top:6px}
.gt{margin:28px 0 12px}
.empty{display:none;color:var(--ink3);padding:30px 0;text-align:center;font-weight:600}
.set.hide,.gt.hide,.grp.hide,.part.hide{display:none}
@media (prefers-reduced-motion:reduce){.scroll{scroll-behavior:auto}}
@media (max-width:1000px){ .snav{display:none} .scroll{padding:18px 16px 60px} }
'''
# three ways to separate things without lines
STYLE = {
 'a': '''
.grp{background:none;padding:0;display:flex;flex-direction:column;gap:6px}
.set{background:var(--sunk);border-radius:16px;padding:14px 18px}
.set:hover{background:color-mix(in srgb,var(--sunk) 70%,var(--line))}
.grp .warnl{margin:0}
.hostr{background:var(--sunk);border-radius:16px;padding:12px 18px}
''',
 'b': '''
.grp{background:var(--sunk);border-radius:22px;padding:8px 22px}
.set{padding:16px 0}
.set + .set{box-shadow:none}
.grp .warnl{margin:12px 0 4px}
.sel,.inp,.seg2{background:var(--surface)}
.seg2 button.on{background:var(--sunk)}
.hostr{padding:12px 0}
''',
 'c': '''
.grp{background:none;padding:0}
.set{padding:14px 14px;margin:0 -14px;border-radius:14px}
.set:hover{background:var(--sunk)}
.set .tx b{font-size:15px}
.gt{font-size:20px;margin-top:40px}
.hostr{padding:10px 14px;margin:0 -14px;border-radius:14px}
.hostr:hover{background:var(--sunk)}
''',
}

def navitem(k, on):
    title, ic, c, admin, _ = R.SEC[k]
    return f'<a class="ni{" on" if on else ""}" href="#s-{k}" data-s="{k}">{R.ft(c)}{I(ic)}</span>{title}{" "+I("lock") if admin else ""}</a>'

JS = '''<script>
(function(){
const sc=document.querySelector('.scroll'),nav=document.querySelectorAll('.ni');
document.querySelectorAll('.sw').forEach(s=>s.addEventListener('click',()=>{s.classList.toggle('on');const t=document.querySelector('.toast');t.hidden=false;clearTimeout(t._h);t._h=setTimeout(()=>t.hidden=true,2600)}));
nav.forEach(a=>a.addEventListener('click',e=>{e.preventDefault();const t=document.getElementById('s-'+a.dataset.s);sc.scrollTo({top:t.offsetTop-sc.offsetTop-10})}));
sc.addEventListener('scroll',()=>{let cur=null;document.querySelectorAll('.gt[id]').forEach(g=>{if(g.offsetTop-sc.offsetTop-40<=sc.scrollTop)cur=g.id.slice(2)});
  nav.forEach(a=>a.classList.toggle('on',a.dataset.s===cur))});
const q=document.querySelector('.hs input');
q.addEventListener('input',()=>{const v=q.value.trim().toLowerCase();let any=false;
  document.querySelectorAll('.gt[id]').forEach(g=>{const grp=g.nextElementSibling;let hit=false;
    grp.querySelectorAll('.set').forEach(s=>{const m=!v||s.textContent.toLowerCase().includes(v)||g.textContent.toLowerCase().includes(v);s.classList.toggle('hide',!m);hit=hit||m});
    if(!grp.querySelector('.set'))hit=!v||grp.textContent.toLowerCase().includes(v);
    g.classList.toggle('hide',!hit);grp.classList.toggle('hide',!hit);any=any||hit});
  document.querySelectorAll('.part').forEach(p=>p.classList.toggle('hide',!!v));
  document.querySelector('.empty').style.display=any?'none':'block'});
})();
</script>'''

def build(v):
    body = (f'<div class="part">You</div>' + ''.join(R.section(k) for k in R.YOU)
          + f'<div class="part">This server, admins only</div>' + ''.join(R.section(k) for k in R.SRV)
          + f'<div class="part">Quadro</div>' + R.section('about'))
    main = top('Search settings') + f'''
    <section class="fm st">
      <nav class="snav"><h2>Settings</h2>
        <div class="gl">You</div>{''.join(navitem(k, k=='appearance') for k in R.YOU)}
        <div class="gl">This server</div>{''.join(navitem(k, False) for k in R.SRV)}
        <div class="gl">Quadro</div>{navitem('about', False)}
      </nav>
      <div class="scroll"><div class="inner">
        <label class="hs">{I('search')}<input placeholder="Search settings, e.g. root, port, theme"></label>
        {body}
        <div class="empty">No setting matches. Try another word.</div>
      </div></div>
    </section>{R.TOAST.replace('<div class="toast">','<div class="toast" hidden>')}'''
    return R.spage('v' + v, R.ST_CSS + LAYOUT + NOLINES + STYLE[v] + '.toast[hidden]{display:none}', main, JS)

if __name__ == '__main__':
    write('020-settings-remix', {k: build(k) for k in 'abc'})
