import sys, os, json
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *
import r19_settings as R19
import r20_settings_remix as R20

HK = ['ov','term','file','log','svc','sw','usr','plg']
def T(name, kind, bg, surf, sunk, line, ink, ink2, ink3, hues, distro, note='', own=False):
    return dict(name=name, kind=kind, note=note, own=own, bg=bg, surface=surf, sunk=sunk, line=line, ink=ink, ink2=ink2, ink3=ink3,
                hues=dict(zip(HK, hues)), distro=distro)
THEMES = [
 T('OLED','dark','#000000','#0E0E10','#18181B','#26262B','#F4F4F6','#A3A3AD','#6C6C76',
   ['#8B93FF','#3DDC97','#5AB0FF','#FFB547','#FF6B81','#B98CFF','#3FD8DE','#FF7AC6'],'#3AB4F2','Default'),
 T('OLED Mono','dark','#000000','#0E0E10','#18181B','#26262B','#F4F4F6','#A3A3AD','#6C6C76',
   ['#E4E4E7']*8,'#E4E4E7','',True),
 T('Midnight','dark','#13151E','#1C1F2B','#242836','#2C3040','#E8EAF2','#A6ABBE','#6E7389',
   ['#9BA3FF','#6FDDB0','#7DB6FF','#F5B963','#FF97A7','#C6A2FF','#6ADCDF','#FF9FD2'],'#4FB8EC'),
 T('Graphite','dark','#1A1A1C','#232326','#2C2C30','#37373C','#EDEDEF','#A9A9B0','#75757D',
   ['#8E95F0','#4FCB91','#63A9EE','#EDB25A','#EE7485','#B395F0','#4CC9CE','#EC83BF'],'#3AB4F2'),
 T('Fjord','dark','#232A36','#2C3442','#343D4D','#3E4859','#ECEFF4','#B4BCCB','#7E889A',
   ['#88A6E0','#A3D49A','#81C8D8','#EBCB8B','#E48A92','#C29BD0','#8FD0C9','#D99AC0'],'#88C0D0','',True),
 T('Forest','dark','#0F1A16','#15231E','#1C2E27','#263A32','#E6F0EB','#A4B8AE','#6E8479',
   ['#9FB4FF','#6FE3A8','#7CC4F0','#F2C46B','#FF8F8F','#C7A6FF','#5ED9C6','#F59BCB'],'#3AB4F2','',True),
 T('High contrast','dark','#000000','#000000','#111111','#FFFFFF','#FFFFFF','#E6E6E6','#BDBDBD',
   ['#A5ABFF','#4DFFB0','#6EC1FF','#FFC94D','#FF7A8C','#D0A6FF','#4DF4FA','#FF8FD6'],'#4FC3F7','Accessibility',True),
 T('Daylight','light','#ECEEF4','#FFFFFF','#F3F4F8','#E1E4EC','#1D2030','#575C70','#8D92A5',
   ['#4651D0','#16805A','#2C73C9','#A9620D','#B5324A','#7A43C2','#0E8A8F','#C04F8E'],'#1793D1'),
 T('Linen','light','#F2EFE9','#FBFAF7','#EEEAE2','#E2DCD1','#2A2620','#625B50','#948B7E',
   ['#4B55C4','#2E7D50','#2F6FB0','#A35F12','#B23A44','#7B4AB8','#187F86','#B3487F'],'#1793D1'),
]

ENGINE = '''<script>
const THEMES = %s;
let MODE='sections', CUR=THEMES[0];
let DISTRO={name:'Arch Linux',c:'#1793D1'};
function eff(t){if(t.own||MODE==='sections')return t.hues;const o={};
  const c=MODE==='mono'?(t.kind==='dark'?'#D4D4D8':'#3F3F46'):(t.kind==='dark'?`color-mix(in srgb,${DISTRO.c} 85%%,#fff)`:DISTRO.c);
  for(const k in t.hues)o[k]=c;return o}
function vars(t){const H=eff(t),dc=t.own?t.distro:(MODE==='mono'?H.ov:DISTRO.c);
  const v={'--bg':t.bg,'--surface':t.surface,'--sunk':t.sunk,'--line':t.line,'--ink':t.ink,'--ink2':t.ink2,'--ink3':t.ink3,'--distro':dc};
  const mix=t.kind==='dark'?t.bg:'#ffffff', p=t.kind==='dark'?18:14;
  for(const k in H){v['--h-'+k]=H[k];v['--h-'+k+'-s']=`color-mix(in srgb,${H[k]} ${p}%%,${mix})`}
  v['--distro-s']=`color-mix(in srgb,${dc} ${p}%%,${mix})`;return v}
function applyTheme(t,el){CUR=t;el=el||document.querySelector('.q');const v=vars(t);for(const k in v)el.style.setProperty(k,v[k]);
  el.style.colorScheme=t.kind}
function swatches(t){return [t.bg,t.surface,...Object.values(eff(t)).slice(0,5)]}
function mini(t){const v=vars(t),H=eff(t);return `<div class="mini" style="background:${t.bg}">
  <div class="mr" style="background:${t.surface}">${Object.values(H).slice(0,5).map(h=>`<i style="background:${h}"></i>`).join('')}</div>
  <div class="mc" style="background:${t.surface}"><b style="background:${t.ink}"></b>
    <div class="ms"><i style="background:${v['--h-ov-s']}"><u style="background:${H.ov}"></u></i><i style="background:${v['--h-log-s']}"><u style="background:${H.log}"></u></i><i style="background:${v['--h-term-s']}"><u style="background:${H.term}"></u></i></div>
    <span style="background:${t.sunk}"></span></div></div>`}
window.RERENDER=[];
function refresh(){applyTheme(CUR);RERENDER.forEach(f=>f())}
applyTheme(THEMES[0]);
document.addEventListener('click',e=>{const m=e.target.closest('[data-mode]');if(m){MODE=m.dataset.mode;m.parentNode.querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===m));
    document.querySelector('.drow').classList.toggle('dim',MODE!=='distro');refresh()}
  const d=e.target.closest('[data-distro]');if(d){DISTRO={name:d.textContent.trim(),c:d.dataset.distro};d.parentNode.querySelectorAll('[data-distro]').forEach(x=>x.classList.toggle('on',x===d));
    MODE='distro';document.querySelectorAll('[data-mode]').forEach(x=>x.classList.toggle('on',x.dataset.mode==='distro'));document.querySelector('.drow').classList.remove('dim');refresh()}});
</script>''' % json.dumps(THEMES)

PICK_CSS = '''
.mini{height:84px;border-radius:12px;display:grid;grid-template-columns:20px 1fr;gap:5px;padding:5px;box-sizing:border-box;overflow:hidden}
.mini .mr{border-radius:7px;display:flex;flex-direction:column;align-items:center;gap:4px;padding:5px 0}
.mini .mr i{width:9px;height:9px;border-radius:3px;display:block}
.mini .mc{border-radius:7px;padding:6px;display:flex;flex-direction:column;gap:5px}
.mini .mc b{display:block;height:5px;width:46%;border-radius:3px;opacity:.85}
.mini .ms{display:grid;grid-template-columns:repeat(3,1fr);gap:4px;flex:1}
.mini .ms i{border-radius:5px;display:flex;align-items:flex-end;padding:3px}
.mini .ms u{display:block;height:4px;width:60%;border-radius:2px}
.mini .mc span{display:block;height:10px;border-radius:4px}
.tgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:12px}
.tc{border-radius:18px;padding:6px;cursor:pointer;outline:2px solid transparent;outline-offset:0;background:var(--sunk);text-align:left}
.tc:hover{outline-color:var(--line)}
.tc.on{outline-color:var(--h-ov)}
.tc .nm{display:flex;align-items:center;justify-content:space-between;padding:9px 6px 4px;font-weight:700;font-size:13.5px}
.tc .nm small{font-size:11.5px;color:var(--ink3);font-weight:700}
.tc .nm .ck{width:18px;height:18px;border-radius:50%;background:var(--h-ov);color:var(--surface);display:none;place-items:center}
.tc.on .nm .ck{display:grid}
.tc .ck .i{width:12px;height:12px}
.drow{display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-top:12px;transition:opacity .15s}
.drow.dim{opacity:.55}
.drow .dl{font-size:13px;color:var(--ink3);margin-right:4px}
.drow .dl b{color:var(--ink)}
.dc{display:inline-flex;align-items:center;gap:7px;height:32px;padding:0 12px 0 8px;border-radius:999px;background:var(--sunk);font-weight:700;font-size:12.5px}
.dc i{width:14px;height:14px;border-radius:50%}
.dc.on{box-shadow:inset 0 0 0 2px var(--ink2)}
.own{font-size:10.5px;font-weight:800;color:var(--ink3);background:var(--surface);padding:1px 6px;border-radius:6px}
.gl2{font-size:12.5px;color:var(--ink3);font-weight:700;margin:16px 0 8px}
'''

DISTROS = ''.join(f'<button class="dc{" on" if n=="Arch" else ""}" data-distro="{c}"><i style="background:{c}"></i>{n}</button>' for n,c in [('Arch', '#1793D1'), ('Ubuntu', '#E95420'), ('Mint', '#87CF3E'), ('Fedora', '#51A2DA'), ('Debian', '#D70A53'), ('openSUSE', '#73BA25'), ('Manjaro', '#35BF5C'), ('Pop!_OS', '#48B9C7')])
def appearance(picker):
    return f'''<div class="set" style="display:block"><div class="tx" style="margin-bottom:14px"><b>Theme</b><small>Saved to your account. Plugins can add more themes.</small></div>{picker}</div>
  <div class="set" style="display:block"><div class="tx" style="margin-bottom:12px"><b>Colours</b><small>Per section gives every area its own colour. Distro colour uses the colour of your distribution everywhere. Themes marked “own colours” keep theirs.</small></div>
    <span class="seg2"><button class="on" data-mode="sections">Per section</button><button data-mode="distro">Distro colour</button><button data-mode="mono">Monochrome</button></span>
    <div class="drow dim"><span class="dl">Detected: <b>Arch Linux</b>. Preview another:</span>
      {DISTROS}</div></div>
  {R19.S('Follow the device','Switch between a dark and a light theme with your system setting.',R19.SW(False))}
  {R19.S('Density','Compact fits more rows in tables and lists.',R19.SEG(['Comfortable','Compact'],'Comfortable'))}
  {R19.S('Reduce motion','Turns off widget wiggle, pulses and transitions.',R19.SW(False))}'''

GRID_JS = '''<script>
(function(){const box=document.querySelector('[data-themes]');const grp=k=>THEMES.map((t,i)=>[t,i]).filter(([t])=>t.kind===k);
const card=([t,i])=>`<button class="tc" data-i="${i}">${mini(t)}<div class="nm">${t.name}${t.note?` <small>${t.note}</small>`:''}${t.own?' <span class="own">own colours</span>':''}<span class="ck"><svg class="i"><use href="#i-check"/></svg></span></div></button>`;
let sel=0;
function render(){box.innerHTML=`<div class="gl2">Dark</div><div class="tgrid">${grp('dark').map(card).join('')}</div><div class="gl2">Light</div><div class="tgrid">${grp('light').map(card).join('')}</div>`;
  box.querySelectorAll('.tc').forEach(x=>x.classList.toggle('on',+x.dataset.i===sel))}
render();RERENDER.push(render);
box.addEventListener('click',e=>{const b=e.target.closest('.tc');if(!b)return;sel=+b.dataset.i;box.querySelectorAll('.tc').forEach(x=>x.classList.toggle('on',x===b));applyTheme(THEMES[sel])});})();
</script>'''

# B: list with swatches + big preview, Apply to commit
LIST_CSS = '''
.tl{display:grid;grid-template-columns:minmax(220px,300px) 1fr;gap:16px;align-items:start}
.tlist{display:flex;flex-direction:column;gap:4px}
.tr{display:flex;align-items:center;gap:12px;padding:10px 12px;border-radius:14px;text-align:left;font-weight:700}
.tr:hover{background:var(--sunk)}
.tr.on{background:var(--sunk);box-shadow:inset 3px 0 0 var(--h-ov)}
.tr .sws{display:flex;margin-left:auto}
.tr .sws i{width:14px;height:14px;border-radius:50%;margin-left:-4px;box-shadow:0 0 0 2px var(--surface)}
.tr small{color:var(--ink3);font-size:11.5px;font-weight:700}
.tprev{border-radius:20px;padding:12px;background:var(--sunk);position:sticky;top:0}
.tprev .mini{height:220px;border-radius:14px;grid-template-columns:44px 1fr;gap:8px;padding:8px}
.tprev .mini .mr i{width:18px;height:18px;border-radius:6px}
.tprev .mini .mc{padding:12px;gap:10px}
.tprev .mini .mc b{height:9px}
.tprev .mini .ms u{height:7px}
.tprev .mini .mc span{height:30px}
.tprev .row{display:flex;align-items:center;gap:10px;margin-top:12px}
.tprev .row b{font-size:16px;font-weight:800}
.tprev .row .abtn{margin-left:auto}
.tprev .cur{font-size:12.5px;color:var(--ink3)}
@media (max-width:1100px){ .tl{grid-template-columns:1fr} }
'''
LIST_JS = '''<script>
(function(){const box=document.querySelector('[data-themes]');let cur=0,sel=0;
box.innerHTML=`<div class="tl"><div class="tlist">${THEMES.map((t,i)=>`<button class="tr${i===0?' on':''}" data-i="${i}">${t.name}${t.note?` <small>${t.note}</small>`:''}${t.own?' <span class="own">own colours</span>':''}<span class="sws">${swatches(t).map(c=>`<i style="background:${c}"></i>`).join('')}</span></button>`).join('')}</div>
<div class="tprev"><div class="pv"></div><div class="row"><div><b class="pn"></b><div class="cur"></div></div><button class="abtn p ap">Use this theme</button></div></div></div>`;
const pv=box.querySelector('.pv'),pn=box.querySelector('.pn'),cu=box.querySelector('.cur'),ap=box.querySelector('.ap');
function show(i){sel=i;pv.innerHTML=mini(THEMES[i]);pn.textContent=THEMES[i].name;cu.textContent=i===cur?'In use':'Preview, not applied yet';ap.hidden=i===cur}
box.addEventListener('click',e=>{const r=e.target.closest('.tr');if(r){box.querySelectorAll('.tr').forEach(x=>x.classList.toggle('on',x===r));show(+r.dataset.i)}
  if(e.target.closest('.ap')){cur=sel;applyTheme(THEMES[cur]);show(cur)}});
show(0);RERENDER.push(()=>{box.querySelectorAll('.tr').forEach(r=>{const t=THEMES[+r.dataset.i];r.querySelector('.sws').innerHTML=swatches(t).map(c=>`<i style="background:${c}"></i>`).join('')});show(sel)});})();
</script>'''

# C: grid + create your own
EDIT_CSS = '''
.ed{margin-top:18px;border-radius:20px;background:var(--sunk);padding:16px 18px;display:flex;flex-direction:column;gap:14px}
.ed .hd{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.ed .hd b{font-size:16px;font-weight:800}
.ed .hd .r{margin-left:auto;display:flex;gap:6px}
.ed .abtn{background:var(--surface)}
.ed .abtn.p{background:var(--h-ov)}
.cols2{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:8px}
.cf{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:12px;background:var(--surface);font-weight:600;font-size:13px;cursor:pointer}
.cf input{width:28px;height:28px;border:0;padding:0;background:none;border-radius:8px;cursor:pointer}
.cf small{display:block;color:var(--ink3);font:11px var(--mono)}
.ed .lb{font-size:12.5px;color:var(--ink3);font-weight:700}
'''
EDIT_JS = GRID_JS + '''<script>
(function(){const box=document.querySelector('[data-themes]');const ed=document.createElement('div');ed.className='ed';
const base=JSON.parse(JSON.stringify(THEMES[0]));base.name='My theme';
const F=[['bg','Background'],['surface','Panels'],['sunk','Cards'],['ink','Text']];const H=[['ov','Overview'],['term','Terminal'],['file','Files'],['log','Logs'],['svc','Services'],['sw','Software'],['usr','Users'],['plg','Plugins']];
const f=(key,label,val)=>`<label class="cf"><input type="color" value="${val}" data-k="${key}"><div>${label}<small>${val.toUpperCase()}</small></div></label>`;
ed.innerHTML=`<div class="hd"><b>Create a theme</b><span class="muted" style="font-size:13px">Starts from OLED. Changes show live.</span><div class="r"><button class="abtn"><svg class="i"><use href="#i-upload"/></svg>Import</button><button class="abtn"><svg class="i"><use href="#i-download"/></svg>Export JSON</button><button class="abtn p">Save as “My theme”</button></div></div>
<div class="lb">Base</div><div class="cols2">${F.map(([k,l])=>f(k,l,base[k])).join('')}</div>
<div class="lb">Section colours</div><div class="cols2">${H.map(([k,l])=>f('h.'+k,l,base.hues[k])).join('')}</div>`;
box.after(ed);
ed.addEventListener('input',e=>{const i=e.target;if(!i.dataset.k)return;const k=i.dataset.k;if(k.startsWith('h.'))base.hues[k.slice(2)]=i.value;else base[k]=i.value;
  if(k==='bg'){base.kind=parseInt(i.value.slice(1,3),16)>128?'light':'dark'}
  i.nextElementSibling.querySelector('small').textContent=i.value.toUpperCase();applyTheme(base);document.querySelectorAll('.tc').forEach(x=>x.classList.remove('on'))});})();
</script>'''

def build(picker_css, js):
    R19.SEC['appearance'] = ('Appearance','palette','t-img', False, appearance('<div data-themes></div>'))
    html = R20.build('c')
    html = html.replace('<style>', '<style>' + PICK_CSS + picker_css, 1)
    return html + ENGINE + js

A = build('', GRID_JS)
B = build(LIST_CSS, LIST_JS)
C = build(EDIT_CSS, EDIT_JS)
write('022-themes', {'a': A, 'b': B, 'c': C})
