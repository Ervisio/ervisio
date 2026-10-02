import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *
import r26_container as C

def nav(item):
    return C.inner_nav().replace('<a class="on">', '<a>', 1).replace(f'<a>{I(item[0])}{item[1]}', f'<a class="on">{I(item[0])}{item[1]}', 1)

# repo, tag, size MB, used by, created, update
IMG = [('ghcr.io/immich-app/immich-machine-learning','v1.140.1',1480,['immich-ml'],'12 days ago',True),
       ('ghcr.io/immich-app/immich-server','v1.140.1',1210,['immich-server'],'12 days ago',True),
       ('nextcloud','31-apache',1150,['nextcloud-app'],'3 weeks ago',True),
       ('ghcr.io/immich-app/immich-server','v1.139.0',1190,[],'5 weeks ago',False),
       ('jellyfin/jellyfin','10.10.7',1020,['jellyfin'],'2 months ago',False),
       ('grafana/grafana','12.1.0',690,['grafana'],'1 month ago',False),
       ('prom/prometheus','v3.5.0',300,['prometheus'],'1 month ago',False),
       ('mariadb','11.4',410,['nextcloud-db'],'3 weeks ago',False),
       ('&lt;none&gt;','&lt;none&gt;',840,[],'2 months ago',False),
       ('vaultwarden/server','1.34.3',92,['vaultwarden'],'4 days ago',False)]
VOL = [('immich_pgdata','6.2 GB',['immich-postgres']),('nextcloud_html','1.1 GB',['nextcloud-app']),('immich_model-cache','2.4 GB',['immich-ml']),
       ('grafana-storage','84 MB',['grafana']),('3f9c2a…e81 (anonymous)','612 MB',[]),('prometheus_data','3.0 GB',['prometheus'])]
SPACE = [('Images','17.4 GB','5.1 GB','sw'),('Containers','1.2 GB','0.4 GB','file'),('Volumes','13.4 GB','0.6 GB','term'),('Build cache','6.0 GB','6.0 GB','log')]

CSS = C.CSS + '''
.h-file{--h:var(--h-file)}.h-sw{--h:var(--h-sw)}.h-term{--h:var(--h-term)}.h-log{--h:var(--h-log)}
.used{display:flex;gap:4px;flex-wrap:wrap}
.used span{font-size:12px;font-weight:700;padding:2px 8px;border-radius:8px;background:var(--acc-s);color:var(--acc)}
.used span.unused,.unused{font-size:12px;font-weight:800;padding:2px 8px;border-radius:8px;background:var(--warn-s);color:var(--warn)}
table.dk{width:100%;border-collapse:separate;border-spacing:0 3px;font-size:13.5px}
.dk th{text-align:left;font-weight:700;color:var(--ink3);font-size:12.5px;padding:8px 10px}
.dk td{padding:9px 10px;background:var(--sunk)}
.dk tr td:first-child{border-radius:12px 0 0 12px}.dk tr td:last-child{border-radius:0 12px 12px 0}
.dk tr:hover td{background:color-mix(in srgb,var(--sunk) 70%,var(--line))}
.dk .repo{font-family:var(--mono);font-size:12.5px}
.dk .tag{font:600 12px var(--mono);padding:2px 7px;border-radius:7px;background:var(--surface);color:var(--ink2)}
.dk .num{text-align:right;white-space:nowrap;font-family:var(--mono);font-size:12.5px}
.sbar{display:flex;height:14px;border-radius:7px;overflow:hidden;gap:3px}
.sbar i{display:block;height:100%;background:var(--h)}
.sbar i.f{opacity:.35}
.leg{display:flex;gap:18px;flex-wrap:wrap}
.leg div{display:flex;align-items:center;gap:8px;font-size:13px}
.leg .sw2{width:12px;height:12px;border-radius:4px;background:var(--h)}
.leg b{font-weight:800}
.dk-main .card{height:auto;flex:none}.leg .muted{font-size:12.5px}
'''

def img_rows(sel=()):
    out=''
    for repo,tag,mb,used,cr,upd in IMG:
        u = ''.join(f'<span>{x}</span>' for x in used) if used else '<span class="unused">Unused</span>'
        up = f'<span class="dk-upd">{I("download")}Newer tag</span>' if upd else ''
        out += f'<tr><td><span class="repo">{repo}</span></td><td><span class="tag">{tag}</span></td><td><div class="used">{u}</div></td><td>{up}</td><td class="muted">{cr}</td><td class="num">{mb/1000:.2f} GB</td><td><div class="dk-act"><button title="Pull again">{I("download")}</button><button title="Run a container">{I("play")}</button><button title="Layers">{I("layers2")}</button><button title="Remove">{I("trash")}</button></div></td></tr>'
    return out
def space_bar():
    tot = 38.0
    bars = ''.join(f'<i class="h-{h}" style="flex:{float(s.split()[0])-float(f.split()[0])}"></i><i class="h-{h} f" style="flex:{float(f.split()[0])}"></i>' for n,s,f,h in SPACE)
    leg = ''.join(f'<div class="h-{h}"><span class="sw2"></span><b>{n}</b> {s}<span class="muted">({f} can be freed)</span></div>' for n,s,f,h in SPACE)
    return f'<div class="sbar">{bars}</div><div class="leg">{leg}</div>'

def wrap(item, head, main, cls, extra=''):
    return fpage(cls, CSS + extra, top('Search containers, images, stacks') + f'''
    <section class="dk-wrap"><div class="dk-body">{nav(item)}<div class="dk-main">{head}{main}</div></div></section>''', js=C.JS)

def head(icon, title, sub, btns):
    return f'<div class="ch"><div class="ttl"><span class="ic">{I(icon)}</span><div><h2>{title}</h2><span class="muted">{sub}</span></div></div><div class="sp"></div>{btns}</div>'

# A: Images page with space bar on top and a guided Clean up dialog look
A = wrap(('image','Images'), head('image','Images','27 images, 17.4 GB. 3 have a newer tag.', f'<button class="b">{I("broom")}Clean up</button><button class="b p">{I("download")}Pull image</button>'),
 f'''<div class="card"><h3>Disk used by Docker<span class="r"><span class="muted" style="font-weight:600;font-size:12.5px">38 GB total, 12.1 GB can be freed</span></span></h3>{space_bar()}</div>
  <div class="card"><div class="bar1" style="display:flex;gap:8px;flex-wrap:wrap"><div class="in" style="flex:1;min-width:220px">{I('search')}<input placeholder="Filter images"></div><span class="seg2"><button class="on">All</button><button>In use</button><button>Unused</button><button>Updates</button></span></div>
  <table class="dk"><thead><tr><th>Image</th><th>Tag</th><th>Used by</th><th></th><th>Created</th><th class="num">Size</th><th></th></tr></thead><tbody>{img_rows()}</tbody></table></div>''', 'va',
 '.seg2{display:inline-flex;background:var(--sunk);border-radius:12px;padding:3px;gap:2px}.seg2 button{height:32px;padding:0 12px;border-radius:9px;font-weight:700;font-size:13px;color:var(--ink3)}.seg2 button.on{background:var(--surface);color:var(--ink)}')

# B: one Storage page: overview + tabs images/volumes/build cache
VROWS = ''.join(f'<tr><td><span class="repo">{n}</span></td><td><div class="used">{"".join(f"<span>{x}</span>" for x in u) if u else "<span class=unused>Unused</span>"}</div></td><td class="num">{s}</td><td><div class="dk-act"><button title="Browse files">{I("files")}</button><button title="Back up">{I("archive")}</button><button title="Remove">{I("trash")}</button></div></td></tr>' for n,s,u in VOL)
B = wrap(('disk','Volumes'), head('disk','Storage','Images, volumes and build cache in one place.', f'<button class="b">{I("broom")}Clean up 12.1 GB</button>'),
 f'''<div class="card">{space_bar()}</div>
  {C.tabs(['Overview','Logs','Settings'],0).replace('>Overview<','>Volumes<').replace('>Logs<','>Images<').replace('>Settings<','>Build cache<')}
  <div class="pane on" data-p="overview" style="flex-direction:column"><div class="card"><table class="dk"><thead><tr><th>Volume</th><th>Used by</th><th class="num">Size</th><th></th></tr></thead><tbody>{VROWS}</tbody></table></div></div>
  <div class="pane" data-p="logs" style="flex-direction:column"><div class="card"><table class="dk"><thead><tr><th>Image</th><th>Tag</th><th>Used by</th><th></th><th>Created</th><th class="num">Size</th><th></th></tr></thead><tbody>{img_rows()}</tbody></table></div></div>
  <div class="pane" data-p="settings" style="flex-direction:column"><div class="card"><h3>Build cache</h3><p class="muted" style="margin:0">6.0 GB from 214 build steps. None is used by a running build.</p><button class="b" style="align-self:flex-start">{I('broom')}Clear build cache</button></div></div>''', 'vb')

# C: Cleanup-first: reclaimable items with checkboxes and a running total
GROUPS = [('Unused images','5.1 GB','sw',[('ghcr.io/immich-app/immich-server:v1.139.0','1.19 GB'),('&lt;none&gt; (dangling image)','840 MB'),('8 more unused images','3.07 GB')],True),
          ('Build cache','6.0 GB','log',[('214 cached build steps','6.0 GB')],True),
          ('Stopped containers','0.4 GB','file',[('node-exporter, exited 2 hours ago','12 MB'),('3 old one-off containers','390 MB')],False),
          ('Unused volumes','612 MB','term',[('3f9c2a…e81 (anonymous)','612 MB')],False)]
def grp(n,s,h,items,on):
    its=''.join(f'<div class="it"><span class="cbx{" on" if on else ""}">{I("check") if on else ""}</span><span class="mono">{a}</span><span class="sp"></span><span class="mono">{b}</span></div>' for a,b in items)
    return f'<div class="grp h-{h}"><div class="gh"><span class="cbx{" on" if on else ""}">{I("check") if on else ""}</span><span class="sw2"></span><b>{n}</b><span class="sp"></span><b class="mono">{s}</b></div>{its}</div>'
C_ = wrap(('broom','Cleanup'), head('broom','Cleanup','Free space Docker no longer needs. Nothing in use is touched.', ''),
 f'''<div class="cl2"><div class="card">{''.join(grp(*g) for g in GROUPS)}
   <p class="muted" style="margin:0;font-size:12.5px">Volumes are never selected by default: they can hold data you still want.</p></div>
  <div class="side3"><div class="card"><small class="muted" style="font-weight:700">Selected</small><b class="big">11.1 GB</b>{space_bar()}<button class="b p">{I('broom')}Free 11.1 GB</button>
   <label class="lab" style="font-size:13.5px"><span class="cb"></span>Run this every week</label></div></div></div>''', 'vc', '''
.cl2{display:grid;grid-template-columns:1.5fr 1fr;gap:12px;align-items:start}
.grp{display:flex;flex-direction:column;gap:3px}.grp+.grp{margin-top:10px}
.grp .gh{display:flex;align-items:center;gap:10px;padding:10px 12px;border-radius:12px;background:var(--sunk)}
.grp .gh .sw2{width:12px;height:12px;border-radius:4px;background:var(--h)}
.grp .it{display:flex;align-items:center;gap:10px;padding:7px 12px 7px 42px;font-size:12.5px;color:var(--ink2)}
.sp{flex:1}
.cbx{width:18px;height:18px;border-radius:6px;display:grid;place-items:center;box-shadow:inset 0 0 0 2px var(--line);color:#000;flex:none}
.cbx.on{background:var(--acc);box-shadow:none}.cbx .i{width:12px;height:12px}
.side3{position:sticky;top:12px}
.big{font-size:34px;font-weight:800;letter-spacing:-.03em}
.side3 .leg{flex-direction:column;gap:6px}
@media(max-width:1200px){.cl2{grid-template-columns:1fr}}''')

if __name__ == '__main__':
    write('031-docker-storage', {'a': A, 'b': B, 'c': C_})
