import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *
import r26_container as C

def nav():
    return C.inner_nav()

def fld(label, val, hint='', mono=False, w=''):
    h = f'<span class="hint">{hint}</span>' if hint else ''
    return f'<div class="fl"{w}><label>{label}</label><div class="in{" mono" if mono else ""}"><input value="{val}"></div>{h}</div>'
def sel(label, val, hint=''):
    h = f'<span class="hint">{hint}</span>' if hint else ''
    return f'<div class="fl"><label>{label}</label><div class="in sel">{val}{I("chevron")}</div>{h}</div>'

IMAGE = f'''<div class="fl"><div class="in mono">{I('image')}<input value="vaultwarden/server:1.34.3"></div>
  <span class="hint ok">{I('check')} Found on Docker Hub, 1.34.3 is the latest, 92 MB</span></div>'''
PORTS = f'''<div class="pr"><div class="in mono"><input value="8222"></div><span class="ar">{I('right')}</span><div class="in mono"><input value="80"></div><div class="in sel">TCP{I('chevron')}</div><button class="b ic g">{I('trash')}</button></div>
  <div class="hint warn">{I('alert')} Port 8222 is free. Port 8080 would conflict with nextcloud-app.</div>
  <button class="b g addr">{I('plus')}Add port</button>'''
VOLS = f'''<div class="pr"><span class="mt">bind</span><div class="in mono"><input value="/srv/vaultwarden/data"></div><span class="ar">{I('right')}</span><div class="in mono"><input value="/data"></div><button class="b ic g">{I('files')}</button></div>
  <button class="b g addr">{I('plus')}Add volume or folder</button>'''
ENV = f'''<div class="pr"><div class="in mono"><input value="DOMAIN"></div><div class="in mono"><input value="https://vault.fonlogen.it"></div><button class="b ic g">{I('trash')}</button></div>
  <div class="pr"><div class="in mono"><input value="ADMIN_TOKEN"></div><div class="in mono"><input value="••••••••••••"></div><span class="bd n">secret</span></div>
  <div class="pr"><div class="in mono"><input value="SIGNUPS_ALLOWED"></div><div class="in mono"><input value="false"></div><button class="b ic g">{I('trash')}</button></div>
  <button class="b g addr">{I('plus')}Add variable</button><button class="b g addr">{I('upload')}Import .env</button>'''
RUNPREV = '''<pre class="run"><span class="d">docker run -d \\</span>
  --name vaultwarden \\
  --restart unless-stopped \\
  -p 8222:80 \\
  -v /srv/vaultwarden/data:/data \\
  -e DOMAIN=https://vault.fonlogen.it \\
  -e ADMIN_TOKEN=******** \\
  -e SIGNUPS_ALLOWED=false \\
  --memory 512m \\
  vaultwarden/server:1.34.3</pre>'''
COMPOSEPREV = '''<pre class="run"><span class="k">services</span>:
  <span class="k">vaultwarden</span>:
    <span class="k">image</span>: vaultwarden/server:1.34.3
    <span class="k">container_name</span>: vaultwarden
    <span class="k">restart</span>: unless-stopped
    <span class="k">ports</span>: ["8222:80"]
    <span class="k">volumes</span>:
      - /srv/vaultwarden/data:/data
    <span class="k">environment</span>:
      DOMAIN: https://vault.fonlogen.it
      ADMIN_TOKEN: ${ADMIN_TOKEN}
      SIGNUPS_ALLOWED: "false"
    <span class="k">mem_limit</span>: 512m</pre>'''

def section(title, body, sub=''):
    return f'<div class="card"><h3>{title}{"<span class=\"sub\">"+sub+"</span>" if sub else ""}</h3>{body}</div>'
BASIC = f'<div class="g2">{fld("Name","vaultwarden")}{sel("Restart","Unless stopped","Starts again after a reboot, unless you stopped it")}</div>'
NET = f'<div class="g2">{sel("Network","bridge (default)")}{fld("Hostname","vaultwarden")}</div>'
RES = f'<div class="g2">{fld("Memory limit","512 MB","Leave empty for no limit")}{fld("CPU limit","1.0","Number of cores")}</div>'
ADV = f'<div class="adv"><span class="chip">Labels</span><span class="chip">Health check</span><span class="chip">User</span><span class="chip">Capabilities</span><span class="chip">Devices</span><span class="chip">Logging</span><span class="chip">Command</span></div>'

CSS = C.CSS + '''
.cf{display:grid;grid-template-columns:1fr 380px;gap:12px;align-items:start}
.cf .forms{display:flex;flex-direction:column;gap:12px;min-width:0}
.g2{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.card h3 .sub{font-weight:600;font-size:12.5px;color:var(--ink3);margin-left:4px}
.fl .hint{display:flex;align-items:center;gap:6px}
.fl .hint .i{width:14px;height:14px}
.hint.ok{color:var(--ok)}
.hint.warn{display:flex;align-items:center;gap:6px;color:var(--warn);font-size:12.5px}
.hint.warn .i{width:14px;height:14px}
.in.mono input{font-family:var(--mono);font-size:13px}
.pr{display:flex;align-items:center;gap:8px}
.pr .in{flex:1;min-width:0}
.pr .in.sel{flex:none;width:90px}
.pr .ar{color:var(--ink3);display:grid}.pr .ar .i{width:14px;height:14px}
.pr .mt{flex:none}
.addr{align-self:flex-start;height:32px}
.adv{display:flex;gap:6px;flex-wrap:wrap}
.chip{display:inline-flex;align-items:center;height:32px;padding:0 12px;border-radius:10px;background:var(--sunk);font-weight:700;font-size:13px;color:var(--ink2);cursor:pointer}
.chip:hover{color:var(--ink)}
.side2{position:sticky;top:12px;display:flex;flex-direction:column;gap:12px}
.run{margin:0;background:#000;border-radius:14px;padding:12px 14px;font:12px/1.7 var(--mono);color:var(--ink2);white-space:pre;overflow:auto}
.run .d{color:var(--ink3)}.run .k{color:var(--h-file)}
.seg2{display:inline-flex;background:var(--sunk);border-radius:12px;padding:3px;gap:2px}
.seg2 button{height:30px;padding:0 12px;border-radius:9px;font-weight:700;font-size:12.5px;color:var(--ink3)}
.seg2 button.on{background:var(--surface);color:var(--ink)}
.go{display:flex;gap:8px}.go .b{flex:1}
@media(max-width:1200px){.cf{grid-template-columns:1fr}.side2{position:static}}
'''

HEAD = f'''<div class="ch"><button class="b g back">{I('chevron',' style="transform:rotate(90deg)"')}Containers</button>
  <div class="ttl"><span class="ic">{I('plus')}</span><div><h2>New container</h2><span class="muted">From an image, or start from a template</span></div></div><div class="sp"></div>
  <button class="b">{I('store')}Templates</button></div>'''

def wrap(main, cls, extra=''):
    return fpage(cls, CSS + extra, top('Search containers, images, stacks') + f'''
    <section class="dk-wrap"><div class="dk-body">{nav()}<div class="dk-main">{HEAD}{main}</div></div></section>''', js=C.JS)

SUMMARY = f'''<div class="card"><h3>Will run<span class="r"><span class="seg2"><button class="on">docker run</button><button>Compose</button></span></span></h3>{RUNPREV}
  <label class="lab" style="font-size:13.5px"><span class="cb"></span>Create as a stack in /opt/stacks/vaultwarden</label>
  <div class="go"><button class="b">Cancel</button><button class="b p">{I('play')}Create and start</button></div></div>'''

# A: one page, all sections, sticky summary with docker run / compose preview
A = wrap(f'''<div class="cf"><div class="forms">
  {section('Image', IMAGE)}{section('Basics', BASIC)}{section('Ports', PORTS, 'host to container')}{section('Storage', VOLS)}
  {section('Environment', ENV)}{section('Network', NET)}{section('Limits', RES)}{section('Advanced', ADV, 'optional')}
  </div><div class="side2">{SUMMARY}</div></div>''', 'va')

# B: wizard with steps and a running summary card
STEPS = ['Image','Ports and storage','Environment','Network and limits','Review']
def steps(on):
    return '<div class="steps">' + ''.join(f'<div class="stp{" on" if i==on else (" done" if i<on else "")}"><span>{I("check") if i<on else i+1}</span>{t}</div>' for i,t in enumerate(STEPS)) + '</div>'
B = wrap(f'''{steps(1)}<div class="cf"><div class="forms">
  {section('Ports', PORTS, 'host to container')}{section('Storage', VOLS)}
  <div class="nav2"><button class="b g">{I('chevron',' style="transform:rotate(90deg)"')}Back</button><span class="sp"></span><button class="b p">Next: Environment{I('chevron',' style="transform:rotate(-90deg)"')}</button></div>
  </div><div class="side2"><div class="card"><h3>So far</h3>
    <dl class="kv2"><dt>Image</dt><dd class="mono">vaultwarden/server:1.34.3</dd><dt>Name</dt><dd>vaultwarden</dd><dt>Restart</dt><dd>Unless stopped</dd><dt>Ports</dt><dd>8222 → 80</dd><dt>Storage</dt><dd>1 folder</dd></dl></div></div></div>''', 'vb',
 '''.steps{display:flex;gap:8px;flex-wrap:wrap}
.stp{display:flex;align-items:center;gap:8px;padding:8px 14px 8px 8px;border-radius:12px;background:var(--surface);font-weight:700;color:var(--ink3)}
.stp span{width:26px;height:26px;border-radius:9px;display:grid;place-items:center;background:var(--sunk);font-size:13px}
.stp span .i{width:14px;height:14px}
.stp.done{color:var(--ok)}.stp.done span{background:var(--ok-s)}
.stp.on{color:var(--ink)}.stp.on span{background:var(--acc);color:#000}
.nav2{display:flex;align-items:center}.nav2 .sp{flex:1}''')

# C: compose-first: form and generated compose side by side, editing either updates the other
C_ = wrap(f'''<div class="two">
  <div class="forms">{section('Image', IMAGE)}{section('Basics', BASIC)}{section('Ports', PORTS)}{section('Storage', VOLS)}{section('Environment', ENV)}</div>
  <div class="side2"><div class="card"><h3>compose.yml<span class="r"><span class="muted" style="font-size:12.5px;font-weight:600">edit here or in the form</span></span></h3>{COMPOSEPREV}
    {fld('Stack folder','/opt/stacks/vaultwarden',mono=True)}
    <div class="go"><button class="b">Cancel</button><button class="b p">{I('play')}Deploy stack</button></div></div></div></div>''', 'vc',
 '.two{display:grid;grid-template-columns:1fr 1fr;gap:12px;align-items:start}.two .forms{display:flex;flex-direction:column;gap:12px;min-width:0}@media(max-width:1200px){.two{grid-template-columns:1fr}}')

if __name__ == '__main__':
    write('028-docker-create', {'a': A, 'b': B, 'c': C_})
