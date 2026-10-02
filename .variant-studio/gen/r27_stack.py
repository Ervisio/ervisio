import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *
import r25_docker as R
import r26_container as C

def nav_stacks():
    return C.inner_nav().replace('<a class="on">', '<a>', 1).replace(f'<a>{I("layers2")}Stacks', f'<a class="on">{I("layers2")}Stacks', 1)

COMPOSE = '''<span class="c"># /opt/stacks/immich/compose.yml</span>
<span class="k">name</span>: immich
<span class="k">services</span>:
  <span class="k">immich-server</span>:
    <span class="k">image</span>: ghcr.io/immich-app/immich-server:<span class="v">${IMMICH_VERSION:-release}</span>
    <span class="k">volumes</span>:
      - <span class="s">${UPLOAD_LOCATION}:/usr/src/app/upload</span>
      - <span class="s">/etc/localtime:/etc/localtime:ro</span>
    <span class="k">env_file</span>: <span class="s">.env</span>
    <span class="k">ports</span>:
      - <span class="s">"2283:2283"</span>
    <span class="k">depends_on</span>: [redis, database]
    <span class="k">restart</span>: <span class="s">unless-stopped</span>
<span class="chg">+   <span class="k">healthcheck</span>:</span>
<span class="chg">+     <span class="k">disable</span>: false</span>
  <span class="k">immich-machine-learning</span>:
    <span class="k">image</span>: ghcr.io/immich-app/immich-machine-learning:<span class="v">${IMMICH_VERSION:-release}</span>
    <span class="k">volumes</span>:
      - <span class="s">model-cache:/cache</span>
    <span class="k">restart</span>: <span class="s">unless-stopped</span>
  <span class="k">redis</span>:
    <span class="k">image</span>: valkey/valkey:8-bookworm
  <span class="k">database</span>:
    <span class="k">image</span>: ghcr.io/immich-app/postgres:14-vectorchord
<span class="k">volumes</span>:
  <span class="k">model-cache</span>:'''
def code(src):
    return '<pre class="code">' + ''.join(f'<span class="ln">{i+1}</span>{l}\n' for i,l in enumerate(src.split('\n'))) + '</pre>'
ENVF = '''UPLOAD_LOCATION=/srv/immich/library
DB_DATA_LOCATION=/srv/immich/postgres
IMMICH_VERSION=v1.140.1
DB_PASSWORD=••••••••••
DB_USERNAME=postgres'''
DEPLOY = '''<span class="d">$ docker compose -f /opt/stacks/immich/compose.yml up -d</span>
 Network immich_default  <span class="ok">Running</span>
 Container immich-redis  <span class="ok">Running</span>
 Container immich-postgres  <span class="ok">Healthy</span>
 Container immich-machine-learning  <span class="ok">Running</span>
 Container immich-server  <span class="y">Recreate</span>
 Container immich-server  <span class="ok">Recreated</span>
 Container immich-server  <span class="ok">Started</span>'''

SVCS = [(n,s,h,up,img) for (stk,n,img,s,h,p,cpu,mem,up,upd) in R.CT if stk=='immich']
def svc_list():
    return ''.join(f'<div class="sv"><span class="dot {R.ST[s][0]}"></span><div class="tx"><b>{n}</b><span class="dk-img">{img}</span></div><span class="muted">{up}</span>{R.actions(s)}</div>' for n,s,h,up,img in SVCS)

HEAD = f'''<div class="ch">
  <button class="b g back">{I('chevron',' style="transform:rotate(90deg)"')}Stacks</button>
  <div class="ttl"><span class="ic">{I('layers2')}</span><div><h2>immich <span class="bd ok"><i></i>4 of 4 running</span></h2>
    <span class="dk-img">/opt/stacks/immich/compose.yml</span></div></div>
  <div class="sp"></div>
  <button class="b">{I('download')}Pull</button><button class="b">{I('stop')}Down</button><button class="b">{I('refresh')}Restart</button><button class="b p">{I('play')}Deploy changes</button>
</div>'''
VALID = f'<div class="valid ok">{I("check")}Valid compose file. 1 change: healthcheck added to immich-server.</div>'

EXTRA = '''
.code{margin:0;background:#000;border-radius:14px;padding:12px 0;font:12.5px/1.7 var(--mono);color:var(--ink);overflow:auto;counter-reset:l;white-space:pre}
.code .ln{display:inline-block;width:38px;text-align:right;padding-right:14px;color:var(--ink3);user-select:none}
.code .k{color:var(--h-file)}.code .s{color:var(--h-term)}.code .v{color:var(--h-log)}.code .c{color:var(--ink3)}
.code .chg{background:color-mix(in srgb,var(--ok) 12%,transparent);display:inline-block;width:calc(100% - 52px)}
.envf{margin:0;background:#000;border-radius:14px;padding:12px 14px;font:12.5px/1.7 var(--mono);color:var(--ink2)}
.dep{margin:0;background:#000;border-radius:14px;padding:12px 14px;font:12.5px/1.7 var(--mono);color:var(--ink2);white-space:pre-wrap}
.dep .ok{color:var(--ok)}.dep .y{color:var(--warn)}.dep .d{color:var(--ink3)}
.valid{display:flex;align-items:center;gap:8px;padding:10px 12px;border-radius:12px;font-weight:700;font-size:13px}
.valid.ok{background:var(--ok-s);color:var(--ok)}.valid .i{width:16px;height:16px}
.sv{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:12px;background:var(--sunk)}
.sv .tx{flex:1;min-width:0;overflow:hidden}.sv .tx b{display:block;font-weight:700}.sv .dk-img{display:block}
.sv .dot{width:10px;height:10px;border-radius:50%;background:var(--ink3);flex:none}.sv .dot.ok{background:var(--ok)}
.sv .muted{font-size:12.5px;white-space:nowrap}
.edtabs{display:flex;gap:4px}
.edtabs button{height:30px;padding:0 12px;border-radius:9px;font-weight:700;font-size:13px;color:var(--ink3)}
.edtabs button.on{background:var(--sunk);color:var(--ink)}
.graph{display:flex;align-items:center;justify-content:center;gap:10px;flex-wrap:wrap;padding:8px}
.graph .n{padding:8px 12px;border-radius:12px;background:var(--sunk);font-weight:700;font-size:13px;display:flex;align-items:center;gap:8px}
.graph .n .dot{width:8px;height:8px;border-radius:50%;background:var(--ok)}
.graph .ar{color:var(--ink3)}
'''

def wrap(main, cls, extra=''):
    return fpage(cls, C.CSS + EXTRA + extra, top('Search containers, images, stacks') + f'''
    <section class="dk-wrap"><div class="dk-body">{nav_stacks()}<div class="dk-main">{HEAD}{main}</div></div></section>''', js=C.JS)

# A: editor-centric: compose editor left (tabs compose/.env), services + deploy output right
A = wrap(f'''<div class="ed">
  <div class="card"><div class="edtabs"><button class="on">compose.yml</button><button>.env</button><button>Diff</button></div>{VALID}{code(COMPOSE)}</div>
  <div class="col">
    <div class="card"><h3>Services</h3>{svc_list()}</div>
    <div class="card"><h3>Last deploy<span class="r"><span class="muted" style="font-weight:600;font-size:12.5px">2 min ago</span></span></h3><pre class="dep">{DEPLOY}</pre></div>
  </div></div>''', 'va', '.ed{display:grid;grid-template-columns:1.4fr 1fr;gap:12px;flex:1}.col{display:flex;flex-direction:column;gap:12px;min-width:0}@media(max-width:1200px){.ed{grid-template-columns:1fr}}')

# B: tabs like the container page: Services / Compose / Environment / Logs / Deploy history
B = wrap(f'''{C.tabs(['Overview','Logs','Settings'],0).replace('>Overview<','>Services<').replace('>Settings<','>Compose<')}
  <div class="pane on" data-p="overview" style="flex-direction:column;gap:12px">
    <div class="card"><h3>Services<span class="r"><button class="b g">{I('plus')}Add service</button></span></h3>{svc_list()}</div>
    <div class="card"><h3>How they connect</h3><div class="graph"><span class="n"><i class="dot"></i>immich-server</span><span class="ar">depends on</span><span class="n"><i class="dot"></i>redis</span><span class="n"><i class="dot"></i>database</span><span class="ar">and</span><span class="n"><i class="dot"></i>machine-learning</span><span class="ar">uses volume</span><span class="n">model-cache</span></div></div>
  </div>
  <div class="pane" data-p="logs" style="flex-direction:column"><div class="card">{C.LOGBAR}<div class="lgbox">{C.logs_html()}</div></div></div>
  <div class="pane" data-p="settings" style="flex-direction:column;gap:12px"><div class="card">{VALID}{code(COMPOSE)}</div><div class="card"><h3>.env</h3><pre class="envf">{ENVF}</pre></div></div>''', 'vb')

# C: wizard-like deploy flow: edit -> review diff -> deploy with live output, steps on top
STEPS = ''.join(f'<div class="stp{" on" if i==1 else (" done" if i==0 else "")}"><span>{I("check") if i==0 else i+1}</span>{t}</div>' for i,t in enumerate(['Edit','Review changes','Deploy']))
C_ = wrap(f'''<div class="steps">{STEPS}</div>
  <div class="rev">
    <div class="card"><h3>What changes</h3>
      <div class="chgl"><span class="bd info">Recreate</span><b>immich-server</b><span class="muted">healthcheck added</span></div>
      <div class="chgl"><span class="bd n">Unchanged</span><b>immich-machine-learning, redis, database</b></div>
      <div class="chgl"><span class="bd warn">Pull</span><b>2 images</b><span class="muted">immich-server and machine-learning, 1.1 GB</span></div>
      {VALID}
      <div class="rowb"><button class="b g">Back to editor</button><button class="b p">{I('play')}Deploy now</button></div>
    </div>
    <div class="card"><h3>Diff</h3>{code(COMPOSE)}</div>
  </div>''', 'vc', '''.steps{display:flex;gap:8px}
.stp{display:flex;align-items:center;gap:8px;padding:8px 14px 8px 8px;border-radius:12px;background:var(--surface);font-weight:700;color:var(--ink3)}
.stp span{width:26px;height:26px;border-radius:9px;display:grid;place-items:center;background:var(--sunk);font-size:13px}
.stp span .i{width:14px;height:14px}
.stp.done{color:var(--ok)}.stp.done span{background:var(--ok-s)}
.stp.on{color:var(--ink)}.stp.on span{background:var(--acc);color:#000}
.rev{display:grid;grid-template-columns:1fr 1.3fr;gap:12px}
.chgl{display:flex;align-items:center;gap:10px;padding:10px 12px;border-radius:12px;background:var(--sunk);font-size:13.5px;flex-wrap:wrap}
.chgl b{font-weight:700}
.rowb{display:flex;gap:8px;justify-content:flex-end}
@media(max-width:1200px){.rev{grid-template-columns:1fr}}''')

if __name__ == '__main__':
    write('027-docker-stack', {'a': A, 'b': B, 'c': C_})
