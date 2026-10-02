import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *
import r25_docker as R

NAV = R.SUBNAV
def inner_nav():
    return f'''<nav class="dk-nav"><div class="gl">Workloads</div>{''.join(f'<a class="{"on" if i==0 else ""}">{I(ic)}{n}<b>{c}</b></a>' for i,(ic,n,c) in enumerate(NAV[:2]))}
      <div class="gl">Resources</div>{''.join(f'<a>{I(ic)}{n}<b>{c}</b></a>' for ic,n,c in NAV[2:5])}
      <div class="gl">Tools</div>{''.join(f'<a>{I(ic)}{n}<b>{c}</b></a>' for ic,n,c in NAV[5:])}</nav>'''

LOGS = [
 ('09:41:02.114','info','[Nest] 7  - Starting Nest application...'),
 ('09:41:02.311','info','[Nest] 7  - JobService: Initialized 14 queues'),
 ('09:41:03.020','info','[Nest] 7  - Immich Server is listening on http://[::1]:2283 [v1.140.1]'),
 ('09:44:17.552','info','GET /api/server/ping 200 2ms'),
 ('09:45:30.871','warn','[Nest] 7  - MetadataService: exiftool took 4210ms for IMG_4402.HEIC'),
 ('09:46:02.004','info','POST /api/assets 201 418ms'),
 ('09:46:02.913','info','[Nest] 7  - SmartInfoService: queued 1 asset for CLIP encoding'),
 ('09:47:11.390','err','[Nest] 7  - MediaService: ffmpeg exited with code 1 for VID_0193.MOV (unsupported codec hevc_10bit)'),
 ('09:47:11.392','info','[Nest] 7  - MediaService: retrying transcode with software decoder'),
 ('09:48:40.120','info','GET /api/timeline/buckets 200 31ms'),
 ('09:49:05.666','info','GET /api/assets/statistics 200 12ms'),
]
def logs_html(n=11):
    return ''.join(f'<div class="lg {lv}"><span class="lts">{ts}</span><span class="msg">{m.replace("ffmpeg exited","<mark>ffmpeg</mark> exited")}</span></div>' for ts,lv,m in LOGS[:n])

HEAD = f'''<div class="ch">
  <button class="b g back">{I('chevron',' style="transform:rotate(90deg)"')}Containers</button>
  <div class="ttl"><span class="ic">{I('box')}</span><div><h2>immich-server <span class="bd ok"><i></i>Running</span> <span class="bd ok">Healthy</span></h2>
    <span class="dk-img">ghcr.io/immich-app/immich-server:v1.140.1</span> <span class="muted">in stack</span> <a class="stk">{I('layers2')}immich</a></div></div>
  <div class="sp"></div>
  <span class="dk-upd" style="height:28px;padding:0 10px">{I('download')}v1.141.0 available</span>
  <button class="b">{I('pause')}Pause</button><button class="b">{I('stop')}Stop</button><button class="b">{I('refresh')}Restart</button><button class="b ic" title="More">{I('more')}</button>
</div>'''

FACTS = [('Created','28 Sep, 09:12'),('Started','3 days ago'),('Restart policy','unless-stopped'),('Health check','every 30s, last ok 4s ago'),('IP','172.20.0.5 on immich_default'),('PID','48213')]
PORTS = [('2283','2283/tcp','0.0.0.0')]
MOUNTS = [('/srv/immich/library','/usr/src/app/upload','rw','bind'),('/etc/localtime','/etc/localtime','ro','bind'),('immich_model-cache','/cache','rw','volume')]
ENV = [('DB_HOSTNAME','immich-postgres',False),('DB_USERNAME','postgres',False),('DB_PASSWORD','••••••••••',True),('REDIS_HOSTNAME','immich-redis',False),('UPLOAD_LOCATION','/usr/src/app/upload',False),('TZ','Europe/Rome',False)]

def facts(): return '<dl class="kv2">' + ''.join(f'<dt>{k}</dt><dd>{v}</dd>' for k,v in FACTS) + '</dl>'
def ports(): return ''.join(f'<div class="row"><span class="dk-port">{h}</span>{I("right")}<span class="mono">{c}</span><span class="muted">{ip}</span><a class="open">{I("link")}Open</a></div>' for h,c,ip in PORTS)
def mounts(): return ''.join(f'<div class="row"><span class="mt {t}">{t}</span><span class="mono src">{s}</span>{I("right")}<span class="mono">{d}</span><span class="bd n">{m}</span></div>' for s,d,m,t in MOUNTS)
def env(): return ''.join(f'<div class="row"><span class="mono k">{k}</span><span class="mono v">{v}</span>{"<button class=\"b ic g\" title=\"Show\">"+I("search")+"</button>" if sec else ""}</div>' for k,v,sec in ENV)

def chart(title, val, sub, seed, base, amp, col='acc'):
    return f'''<div class="st"><div class="sh"><small>{title}</small><b>{val}</b><span class="muted">{sub}</span></div>
      <svg class="sc {col}" data-chart="{seed},{base},{amp},1" data-n="80" data-fill="1"></svg></div>'''
STATS = (chart('CPU','12.4%','of 16 cores',4,22,18) + chart('Memory','612 MB','limit none, 4% of host',9,40,4,'mem')
       + chart('Network','1.2 MB/s','in 0.9, out 0.3',12,18,22,'net') + chart('Disk I/O','3.4 MB/s','read 2.9, write 0.5',15,14,18,'io'))

SHELL = '''<div class="term"><span class="g">root@0f3a91c2</span>:<span class="bl">/usr/src/app</span># ls upload
backups  encoded-video  library  profile  thumbs  upload
<span class="g">root@0f3a91c2</span>:<span class="bl">/usr/src/app</span># df -h /usr/src/app/upload
Filesystem      Size  Used Avail Use% Mounted on
/dev/sdb1       3.6T  1.9T  1.7T  53% /usr/src/app/upload
<span class="g">root@0f3a91c2</span>:<span class="bl">/usr/src/app</span># <span class="cur"></span></div>'''

INSPECT = '''<pre class="insp">{
  <span class="k">"Id"</span>: <span class="s">"0f3a91c2d8e4…"</span>,
  <span class="k">"Created"</span>: <span class="s">"2026-09-28T07:12:44Z"</span>,
  <span class="k">"State"</span>: { <span class="k">"Status"</span>: <span class="s">"running"</span>, <span class="k">"Health"</span>: { <span class="k">"Status"</span>: <span class="s">"healthy"</span> } },
  <span class="k">"HostConfig"</span>: { <span class="k">"RestartPolicy"</span>: { <span class="k">"Name"</span>: <span class="s">"unless-stopped"</span> }, <span class="k">"Memory"</span>: 0 },
  <span class="k">"NetworkSettings"</span>: { <span class="k">"Networks"</span>: { <span class="k">"immich_default"</span>: { <span class="k">"IPAddress"</span>: <span class="s">"172.20.0.5"</span> } } }
}</pre>'''

CSS = R.COMMON + '''
.dk-body{flex:1;display:flex;gap:12px;min-height:0}
.dk-nav{width:220px;flex:none;background:var(--surface);border-radius:18px;padding:12px;display:flex;flex-direction:column;gap:2px}
.dk-nav a{display:flex;align-items:center;gap:10px;padding:9px 10px;border-radius:12px;color:var(--ink2);font-weight:700;text-decoration:none}
.dk-nav a .i{width:17px;height:17px}
.dk-nav a b{margin-left:auto;font-size:12px;color:var(--ink3)}
.dk-nav a.on{background:var(--acc-s);color:var(--acc)}.dk-nav a.on b{color:inherit}
.dk-nav .gl{font-size:12px;color:var(--ink3);font-weight:700;margin:12px 10px 4px}
.dk-main{flex:1;min-width:0;display:flex;flex-direction:column;gap:12px}
.ch{display:flex;align-items:center;gap:10px;flex-wrap:wrap;background:var(--surface);border-radius:18px;padding:14px 16px}
.ch .back{height:32px}
.ch .ttl{display:flex;align-items:center;gap:12px;min-width:0}
.ch .ic{width:44px;height:44px;border-radius:14px;display:grid;place-items:center;background:var(--acc-s);color:var(--acc)}
.ch .ic .i{width:22px;height:22px}
.ch h2{margin:0;font-size:20px;font-weight:800;display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.ch .stk{display:inline-flex;align-items:center;gap:5px;color:var(--acc);font-weight:700;font-size:13px;text-decoration:none}
.ch .stk .i{width:13px;height:13px}
.ch .sp{flex:1}
.ctabs{display:flex;gap:4px;background:var(--surface);border-radius:14px;padding:4px;align-self:flex-start;flex-wrap:wrap}
.ctabs button{display:inline-flex;align-items:center;gap:7px;height:34px;padding:0 12px;border-radius:10px;font-weight:700;font-size:13px;color:var(--ink3)}
.ctabs button.on{background:var(--acc);color:#000}
.ctabs .i{width:15px;height:15px}
.pane{display:none}.pane.on{display:flex}
.card{background:var(--surface);border-radius:18px;padding:16px 18px;display:flex;flex-direction:column;gap:10px;min-width:0}
.card h3{margin:0;font-size:15px;font-weight:800;display:flex;align-items:center;gap:8px}
.card h3 .r{margin-left:auto;display:flex;gap:6px}
.kv2{display:grid;grid-template-columns:auto 1fr;gap:8px 16px;font-size:13.5px;margin:0}
.kv2 dt{color:var(--ink3)}.kv2 dd{margin:0;font-weight:600}
.row{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:12px;background:var(--sunk);font-size:13px;min-width:0}
.row > .i{width:14px;height:14px;color:var(--ink3);flex:none}
.row .src{color:var(--ink2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row .open{margin-left:auto;display:inline-flex;align-items:center;gap:5px;color:var(--acc);font-weight:700;text-decoration:none;font-size:12.5px}
.row .open .i{width:13px;height:13px}
.row .k{color:var(--ink);font-weight:600;min-width:150px}
.row .v{color:var(--ink2);flex:1;overflow:hidden;text-overflow:ellipsis}
.row .b.ic{width:28px;height:28px}
.mt{font-size:11px;font-weight:800;padding:2px 7px;border-radius:7px;background:var(--h-log-s);color:var(--h-log)}
.mt.volume{background:var(--h-sw-s);color:var(--h-sw)}
.st{background:var(--sunk);border-radius:14px;padding:12px 14px;display:flex;flex-direction:column;gap:8px;min-width:0}
.st .sh{display:flex;align-items:baseline;gap:8px;flex-wrap:wrap}
.st small{color:var(--ink3);font-weight:700;font-size:12px;width:100%}
.st b{font-size:22px;font-weight:800;letter-spacing:-.02em}
.st .sh .muted{font-size:12px}
.sc{width:100%;height:70px;display:block}
.sc .line{fill:none;stroke-width:1.8;stroke:var(--acc)}.sc .area{fill:var(--acc);opacity:.14}
.sc.mem .line{stroke:var(--h-sw)}.sc.mem .area{fill:var(--h-sw)}
.sc.net .line{stroke:var(--h-term)}.sc.net .area{fill:var(--h-term)}
.sc.io .line{stroke:var(--h-log)}.sc.io .area{fill:var(--h-log)}
.lgbox{background:#000;border-radius:14px;padding:10px 12px;font:12.5px/1.65 var(--mono);overflow:auto;flex:1;min-height:240px}
.lg{display:flex;gap:12px;padding:1px 4px;border-radius:6px}
.lg .lts{color:var(--ink3);flex:none}
.lg .msg{color:var(--ink);white-space:pre-wrap;word-break:break-word}
.lg.warn{background:color-mix(in srgb,var(--warn) 10%,transparent)}.lg.warn .msg{color:var(--warn)}
.lg.err{background:color-mix(in srgb,var(--err) 12%,transparent)}.lg.err .msg{color:var(--err)}
.lg mark{background:var(--acc);color:#000;border-radius:3px;padding:0 2px}
.lgbar{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.lgbar .in{height:34px;flex:1;min-width:180px}
.live{display:inline-flex;align-items:center;gap:7px;height:30px;padding:0 10px;border-radius:9px;background:var(--ok-s);color:var(--ok);font-weight:800;font-size:12.5px}
.live i{width:7px;height:7px;border-radius:50%;background:currentColor}
.term{background:#000;border-radius:14px;padding:12px 14px;font:13px/1.6 var(--mono);color:#E4E4E7;white-space:pre-wrap;min-height:220px;flex:1}
.term .g{color:var(--h-term)}.term .bl{color:var(--h-file)}
.term .cur{display:inline-block;width:8px;height:16px;background:var(--h-term);vertical-align:-3px}
.shbar{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.insp{margin:0;background:#000;border-radius:14px;padding:12px 14px;font:12.5px/1.7 var(--mono);color:var(--ink2);overflow:auto}
.insp .k{color:var(--h-file)}.insp .s{color:var(--h-term)}
.muted{color:var(--ink3)}
@media (max-width:1000px){.dk-nav{display:none}}
'''

JS = '''<script>
document.querySelectorAll('.ctabs').forEach(t=>t.addEventListener('click',e=>{const b=e.target.closest('[data-t]');if(!b)return;
 t.querySelectorAll('button').forEach(x=>x.classList.toggle('on',x===b));
 document.querySelectorAll('.pane').forEach(p=>p.classList.toggle('on',p.dataset.p===b.dataset.t))}));
</script>'''

def tabs(names, on=0):
    icons = {'Overview':'overview','Logs':'logs','Stats':'pulse','Shell':'terminal','Inspect':'code','Settings':'cog','Details':'files'}
    return '<div class="ctabs">' + ''.join(f'<button data-t="{n.lower()}" class="{"on" if i==on else ""}">{I(icons[n])}{n}</button>' for i,n in enumerate(names)) + '</div>'

LOGBAR = f'<div class="lgbar"><div class="in">{I("search")}<input value="ffmpeg"></div><span class="live"><i></i>Live</span><button class="b g">{I("clock")}Last hour</button><button class="b g">{I("download")}Save</button><button class="b ic g" title="Wrap lines">{I("list")}</button></div>'
SHELLBAR = f'<div class="shbar"><span class="muted">Run</span><div class="in sel" style="height:34px;min-width:120px">/bin/bash{I("chevron")}</div><span class="muted">as</span><div class="in sel" style="height:34px;min-width:100px">root{I("chevron")}</div><button class="b">{I("refresh")}Reconnect</button></div>'

def wrap(main, cls, extra_css=''):
    return fpage(cls, CSS + extra_css, top('Search containers, images, stacks') + f'''
    <section class="dk-wrap">
      <div class="dk-body">{inner_nav()}<div class="dk-main">{HEAD}{main}</div></div>
    </section>''', js=JS)

# A: tabs, Overview tab holds stats + facts + ports + mounts + env
A = wrap(f'''{tabs(['Overview','Logs','Stats','Shell','Inspect','Settings'])}
  <div class="pane on" data-p="overview" style="flex-direction:column;gap:12px">
    <div class="g4">{STATS}</div>
    <div class="g2">
      <div class="card"><h3>Container</h3>{facts()}</div>
      <div class="card"><h3>Ports</h3>{ports()}<h3 style="margin-top:6px">Mounts</h3>{mounts()}</div>
    </div>
    <div class="card"><h3>Environment<span class="r"><button class="b g">{I('edit')}Edit</button></span></h3>{env()}</div>
  </div>
  <div class="pane" data-p="logs" style="flex-direction:column;gap:10px"><div class="card" style="flex:1">{LOGBAR}<div class="lgbox">{logs_html()}</div></div></div>
  <div class="pane" data-p="stats" style="flex-direction:column"><div class="g2">{STATS}</div></div>
  <div class="pane" data-p="shell" style="flex-direction:column"><div class="card">{SHELLBAR}{SHELL}</div></div>
  <div class="pane" data-p="inspect" style="flex-direction:column"><div class="card">{INSPECT}</div></div>
  <div class="pane" data-p="settings" style="flex-direction:column"><div class="card"><h3>Recreate with changes</h3><p class="muted" style="margin:0">Opens the container form with the current settings filled in.</p><button class="b p" style="align-self:flex-start">{I('edit')}Edit and recreate</button></div></div>''',
 'va', '.g4{display:grid;grid-template-columns:repeat(4,1fr);gap:10px}.g2{display:grid;grid-template-columns:1fr 1fr;gap:12px}@media(max-width:1200px){.g4{grid-template-columns:1fr 1fr}.g2{grid-template-columns:1fr}}')

# B: logs-first split: big live logs left, details column right, stats strip on top
B = wrap(f'''<div class="strip">{STATS}</div>
  <div class="split">
    <div class="card logs">{tabs(['Logs','Shell','Inspect'])}
      <div class="pane on" data-p="logs" style="flex-direction:column;gap:10px;flex:1">{LOGBAR}<div class="lgbox">{logs_html()}</div></div>
      <div class="pane" data-p="shell" style="flex-direction:column;gap:10px;flex:1">{SHELLBAR}{SHELL}</div>
      <div class="pane" data-p="inspect" style="flex-direction:column;flex:1">{INSPECT}</div>
    </div>
    <div class="side">
      <div class="card"><h3>Container</h3>{facts()}</div>
      <div class="card"><h3>Ports</h3>{ports()}</div>
      <div class="card"><h3>Mounts</h3>{mounts()}</div>
      <div class="card"><h3>Environment<span class="r"><button class="b g">{I('edit')}Edit</button></span></h3>{env()}</div>
    </div>
  </div>''', 'vb', '''.strip{display:grid;grid-template-columns:repeat(4,1fr);gap:10px}.strip .sc{height:44px}
.split{display:grid;grid-template-columns:1.5fr 1fr;gap:12px;flex:1;min-height:0}
.card.logs{min-height:520px}.side{display:flex;flex-direction:column;gap:12px;min-width:0}
.side .row .k{min-width:120px}
@media(max-width:1200px){.split{grid-template-columns:1fr}.strip{grid-template-columns:1fr 1fr}}''')

# C: dashboard: big charts row + two columns (live logs + shell) with details in an accordion
ACC = ''.join(f'<details class="acc"{" open" if i==0 else ""}><summary>{t}{I("chevron")}</summary><div class="ab">{b}</div></details>' for i,(t,b) in enumerate([('Container',facts()),('Ports',ports()),('Mounts',mounts()),('Environment',env()),('Inspect',INSPECT)]))
C = wrap(f'''<div class="big">{STATS}</div>
  <div class="cols">
    <div class="card"><h3>{I('logs')}Logs</h3>{LOGBAR}<div class="lgbox">{logs_html(9)}</div></div>
    <div class="card"><h3>{I('terminal')}Shell</h3>{SHELLBAR}{SHELL}</div>
  </div>
  <div class="card">{ACC}</div>''', 'vc', '''.big{display:grid;grid-template-columns:repeat(4,1fr);gap:10px}.big .sc{height:90px}
.cols{display:grid;grid-template-columns:1fr 1fr;gap:12px}.cols .lgbox,.cols .term{min-height:300px}
.acc{border-radius:12px}.acc+.acc{margin-top:4px}
.acc summary{list-style:none;cursor:pointer;display:flex;align-items:center;gap:8px;padding:10px 12px;border-radius:12px;font-weight:800;background:var(--sunk)}
.acc summary::-webkit-details-marker{display:none}
.acc summary .i{margin-left:auto;width:16px;height:16px;color:var(--ink3);transition:transform .15s}
.acc[open] summary .i{transform:rotate(180deg)}
.acc .ab{padding:10px 4px 4px;display:flex;flex-direction:column;gap:6px}
@media(max-width:1200px){.big{grid-template-columns:1fr 1fr}.cols{grid-template-columns:1fr}}''')

if __name__ == '__main__':
    write('026-docker-container', {'a': A, 'b': B, 'c': C})
