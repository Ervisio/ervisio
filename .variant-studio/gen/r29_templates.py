import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *
import r26_container as C

def nav():
    return C.inner_nav().replace('<a class="on">', '<a>', 1).replace(f'<a>{I("store")}Templates', f'<a class="on">{I("store")}Templates', 1)

# name, category, desc, color hue, source, installs, installed
T = [
 ('Nextcloud','Files','Your own cloud for files, calendar and contacts.','file','LinuxAdmin','12k',True),
 ('Immich','Photos','Photo and video backup from your phone, with face search.','plg','LinuxAdmin','9k',True),
 ('Jellyfin','Media','Stream your movies, shows and music to any device.','sw','LinuxAdmin','15k',True),
 ('Vaultwarden','Security','Password manager compatible with the Bitwarden apps.','term','LinuxAdmin','11k',False),
 ('Home Assistant','Home','Control lights, sensors and automations at home.','usr','LinuxAdmin','14k',False),
 ('Uptime Kuma','Monitoring','Checks your sites and services and alerts when they go down.','term','LinuxAdmin','8k',False),
 ('Paperless-ngx','Documents','Scan, index and search your paper documents.','log','LinuxAdmin','6k',False),
 ('Pi-hole','Network','Block ads for every device on your network.','svc','Portainer list','10k',False),
 ('Gitea','Development','Lightweight Git server with issues and pull requests.','term','Portainer list','5k',False),
 ('Grafana','Monitoring','Dashboards for metrics from Prometheus and more.','log','LinuxAdmin','7k',True),
 ('Syncthing','Files','Sync folders between your devices, peer to peer.','file','Portainer list','4k',False),
 ('Navidrome','Media','Music server with a web player and Subsonic apps.','sw','Portainer list','3k',False),
]
CATS = ['All','Files','Photos','Media','Security','Home','Monitoring','Documents','Network','Development']

CSS = C.CSS + '''
.tic{width:48px;height:48px;border-radius:15px;display:grid;place-items:center;font-weight:800;font-size:20px;flex:none;background:var(--s);color:var(--h)}
.h-file{--h:var(--h-file);--s:var(--h-file-s)}.h-plg{--h:var(--h-plg);--s:var(--h-plg-s)}.h-sw{--h:var(--h-sw);--s:var(--h-sw-s)}.h-term{--h:var(--h-term);--s:var(--h-term-s)}
.h-usr{--h:var(--h-usr);--s:var(--h-usr-s)}.h-log{--h:var(--h-log);--s:var(--h-log-s)}.h-svc{--h:var(--h-svc);--s:var(--h-svc-s)}
.cats{display:flex;gap:6px;flex-wrap:wrap}
.cat{height:34px;padding:0 12px;border-radius:10px;font-weight:700;font-size:13px;color:var(--ink2);background:var(--surface)}
.cat.on{background:var(--acc);color:#000}
.src{font-size:11.5px;font-weight:700;color:var(--ink3)}
.inst{font-size:12px;font-weight:800;color:var(--ok);display:inline-flex;align-items:center;gap:4px}
.inst .i{width:13px;height:13px}
.bar1{display:flex;gap:10px;align-items:center;flex-wrap:wrap}
.bar1 .in{flex:1;min-width:240px}
'''
HEAD = f'''<div class="ch"><div class="ttl"><span class="ic">{I('store')}</span><div><h2>Templates</h2><span class="muted">Apps you can install with a short form. 2 sources: LinuxAdmin catalog (signed) and 1 Portainer list.</span></div></div><div class="sp"></div>
  <button class="b">{I('link')}Sources</button></div>'''

def wrap(main, cls, extra=''):
    return fpage(cls, CSS + extra, top('Search containers, images, stacks') + f'''
    <section class="dk-wrap"><div class="dk-body">{nav()}<div class="dk-main">{HEAD}{main}</div></div></section>''', js=C.JS)

def card(t):
    n,cat,d,h,src,ins,installed = t
    act = f'<span class="inst">{I("check")}Installed</span>' if installed else '<button class="b p sm">Install</button>'
    return f'''<div class="tc h-{h}"><div class="top"><span class="tic">{n[0]}</span><div class="tx"><b>{n}</b><span class="src">{cat}, {src}</span></div></div>
      <p>{d}</p><div class="f"><span class="muted">{ins} installs</span>{act}</div></div>'''

# A: store grid
A = wrap(f'''<div class="bar1"><div class="in">{I('search')}<input placeholder="Search 140 apps"></div></div>
  <div class="cats">{''.join(f'<button class="cat{" on" if i==0 else ""}">{c}</button>' for i,c in enumerate(CATS))}</div>
  <div class="feat h-plg"><span class="tic big">I</span><div><small>Popular this month</small><h3>Immich</h3><p>Back up photos and videos from your phone, browse them on a map and find faces.</p></div><button class="b">Open</button></div>
  <div class="tgrid">{''.join(card(t) for t in T)}</div>''', 'va', '''
.feat{display:flex;align-items:center;gap:18px;padding:20px;border-radius:18px;background:var(--s)}
.feat .tic.big{width:72px;height:72px;font-size:30px;border-radius:22px;background:var(--h);color:#000}
.feat small{color:var(--h);font-weight:800}
.feat h3{margin:2px 0;font-size:22px;font-weight:800}
.feat p{margin:0;color:var(--ink2);max-width:60ch}
.feat .b{margin-left:auto}
.tgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:10px}
.tc{background:var(--surface);border-radius:18px;padding:14px;display:flex;flex-direction:column;gap:10px}
.tc .top{display:flex;align-items:center;gap:12px}
.tc b{display:block;font-weight:800;font-size:15px}
.tc p{margin:0;color:var(--ink2);font-size:13.5px;line-height:1.45;flex:1}
.tc .f{display:flex;align-items:center;justify-content:space-between;font-size:12.5px}
.b.sm{height:32px;font-size:12.5px}''')

# B: list + detail with the install form inline
def row(t,on=False):
    n,cat,d,h,src,ins,installed=t
    return f'<div class="tr h-{h}{" on" if on else ""}"><span class="tic sm">{n[0]}</span><div class="tx"><b>{n}</b><span class="src">{cat}</span></div>{"<span class=\"inst\">"+I("check")+"</span>" if installed else ""}</div>'
DETAIL = f'''<div class="card det h-term"><div class="dh"><span class="tic big">V</span><div><h3>Vaultwarden</h3><span class="src">Security, LinuxAdmin catalog, signed</span></div><span class="sp"></span><a class="b g">{I('link')}Website</a></div>
  <p class="muted" style="margin:0">Password manager that works with the official Bitwarden apps and browser extensions. Runs in one container with about 30 MB of memory.</p>
  <div class="needs"><span class="nd">{I('net')}Port 8222</span><span class="nd">{I('disk')}1 folder</span><span class="nd">{I('lock')}Admin token</span><span class="nd">{I('box')}1 container</span></div>
  <h4>Settings</h4>
  <div class="fl"><label>Address people will use</label><div class="in mono"><input value="https://vault.fonlogen.it"></div><span class="hint">Needed for the apps and for e-mail links.</span></div>
  <div class="g2"><div class="fl"><label>Port on this server</label><div class="in mono"><input value="8222"></div><span class="hint ok">{I('check')} Free</span></div>
  <div class="fl"><label>Data folder</label><div class="in mono"><input value="/srv/vaultwarden"></div></div></div>
  <div class="fl"><label>Admin token</label><div class="in mono"><input value="••••••••••••••••"></div><span class="hint">Generated for you. Shown once after install.</span></div>
  <label class="lab" style="font-size:13.5px"><span class="cb on">{I('check')}</span>Allow new sign-ups</label>
  <div class="go"><button class="b">{I('code')}Show compose</button><button class="b p">{I('download')}Install</button></div></div>'''
B = wrap(f'''<div class="ld"><div class="list"><div class="in">{I('search')}<input placeholder="Search apps"></div>
  <div class="cats sm">{''.join(f'<button class="cat{" on" if i==0 else ""}">{c}</button>' for i,c in enumerate(CATS[:6]))}</div>
  {''.join(row(t, t[0]=='Vaultwarden') for t in T)}</div>{DETAIL}</div>''', 'vb', '''
.ld{display:grid;grid-template-columns:340px 1fr;gap:12px;align-items:start}
.list{background:var(--surface);border-radius:18px;padding:12px;display:flex;flex-direction:column;gap:4px}
.list .in{margin-bottom:6px}
.cats.sm{margin-bottom:6px}.cats.sm .cat{height:30px;font-size:12.5px;background:var(--sunk)}.cats.sm .cat.on{background:var(--acc)}
.tr{display:flex;align-items:center;gap:10px;padding:8px;border-radius:12px;cursor:pointer}
.tr:hover{background:var(--sunk)}.tr.on{background:var(--acc-s)}
.tr .tx{flex:1;min-width:0}.tr b{display:block;font-weight:700}
.tic.sm{width:34px;height:34px;border-radius:11px;font-size:15px}
.tic.big{width:64px;height:64px;font-size:28px;border-radius:20px;background:var(--h);color:#000}
.det .dh{display:flex;align-items:center;gap:14px}.det .dh .sp{flex:1}
.det .fl{flex:none}
.det h3{margin:0;font-size:22px;font-weight:800}
.det h4{margin:6px 0 0;font-size:15px;font-weight:800}
.needs{display:flex;gap:8px;flex-wrap:wrap}
.nd{display:inline-flex;align-items:center;gap:6px;height:32px;padding:0 12px;border-radius:10px;background:var(--sunk);font-weight:700;font-size:13px;color:var(--ink2)}
.nd .i{width:15px;height:15px;color:var(--h)}
.g2{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.hint.ok{color:var(--ok);display:flex;align-items:center;gap:5px}.hint.ok .i{width:13px;height:13px}
.go{display:flex;gap:8px;justify-content:flex-end}
@media(max-width:1100px){.ld{grid-template-columns:1fr}}''')

# C: sources-aware compact grid with filters and source management strip
SRCS = f'''<div class="srcs"><div class="sr on"><span class="tic xs h-file">L</span><div><b>LinuxAdmin catalog</b><span class="muted">Signed, 64 apps, updated today</span></div><span class="sw on"></span></div>
  <div class="sr"><span class="tic xs h-log">P</span><div><b>selfhosted templates</b><span class="muted mono" style="font-size:11.5px">raw.githubusercontent.com/…/templates.json</span></div><span class="sw on"></span></div>
  <button class="sr add">{I('plus')}Add a Portainer template list</button></div>'''
def tile(t):
    n,cat,d,h,src,ins,installed=t
    return f'<div class="tl h-{h}"><span class="tic">{n[0]}</span><b>{n}</b><span class="src">{cat}</span>{"<span class=\"inst\">"+I("check")+"Installed</span>" if installed else "<button class=\"b sm\">Install</button>"}</div>'
C_ = wrap(f'''{SRCS}<div class="bar1"><div class="in">{I('search')}<input placeholder="Search apps"></div><div class="in sel" style="min-width:160px">All categories{I('chevron')}</div><label class="lab" style="font-size:13.5px"><span class="cb"></span>Hide installed</label></div>
  <div class="tiles">{''.join(tile(t) for t in T)}</div>''', 'vc', '''
.srcs{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:10px}
.sr{display:flex;align-items:center;gap:12px;padding:12px;border-radius:16px;background:var(--surface);text-align:left}
.sr b{display:block;font-weight:800}.sr .muted{font-size:12.5px;display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:240px}
.sr .sw{margin-left:auto;transform:scale(.85)}
.sr.add{border:2px dashed var(--line);background:none;justify-content:center;color:var(--ink3);font-weight:700}
.sr.add .i{width:16px;height:16px}
.tic.xs{width:34px;height:34px;border-radius:11px;font-size:15px}
.tiles{display:grid;grid-template-columns:repeat(auto-fill,minmax(170px,1fr));gap:10px}
.tl{background:var(--surface);border-radius:18px;padding:16px 12px;display:flex;flex-direction:column;align-items:center;gap:6px;text-align:center}
.tl .tic{width:60px;height:60px;border-radius:19px;font-size:26px;margin-bottom:4px}
.tl b{font-weight:800}
.tl .b.sm{height:30px;font-size:12.5px;margin-top:4px}
.tl .inst{margin-top:8px}''')

if __name__ == '__main__':
    write('029-docker-templates', {'a': A, 'b': B, 'c': C_})
