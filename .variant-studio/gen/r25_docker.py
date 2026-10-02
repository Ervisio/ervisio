import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from final import *

# stack, name, image, state, health, ports, cpu, mem, uptime, update
CT = [
 ('immich','immich-server','ghcr.io/immich-app/immich-server:v1.140.1','running','healthy','2283:2283',12.4,'612 MB','3 days',True),
 ('immich','immich-ml','ghcr.io/immich-app/immich-machine-learning:v1.140.1','running','healthy','',3.1,'1.4 GB','3 days',True),
 ('immich','immich-redis','valkey/valkey:8-bookworm','running','healthy','',0.2,'14 MB','3 days',False),
 ('immich','immich-postgres','ghcr.io/immich-app/postgres:14-vectorchord','running','healthy','',0.8,'190 MB','3 days',False),
 ('nextcloud','nextcloud-app','nextcloud:31-apache','running','','8080:80',1.9,'240 MB','11 days',True),
 ('nextcloud','nextcloud-db','mariadb:11.4','running','','',0.6,'130 MB','11 days',False),
 ('nextcloud','nextcloud-redis','redis:7-alpine','restarting','','',0.0,'4 MB','restarting (3)',False),
 ('monitoring','grafana','grafana/grafana:12.1.0','running','','3000:3000',0.4,'88 MB','6 hours',False),
 ('monitoring','prometheus','prom/prometheus:v3.5.0','running','','9091:9090',1.1,'210 MB','6 hours',False),
 ('monitoring','node-exporter','prom/node-exporter:v1.9.1','exited','','',0.0,'—','exited (0) 2 hours ago',False),
 ('','vaultwarden','vaultwarden/server:1.34.3','running','unhealthy','8222:80',0.1,'22 MB','5 days',True),
 ('','jellyfin','jellyfin/jellyfin:10.10.7','paused','','8096:8096',0.0,'301 MB','paused',False),
]
ST = {'running':('ok','Running'),'exited':('n','Exited'),'restarting':('warn','Restarting'),'paused':('info','Paused')}

SUBNAV = [('box','Containers','12'),('layers2','Stacks','3'),('image','Images','27'),('disk','Volumes','18'),('net','Networks','6'),('store','Templates',''),('key','Registries','2'),('broom','Cleanup','14 GB')]

COMMON = '''
.dk-wrap{flex:1;min-height:calc(100vh - 90px);display:flex;flex-direction:column;gap:12px}
.dk-head{display:flex;align-items:center;gap:14px;flex-wrap:wrap}
.dk-head h1{margin:0;font-size:26px;font-weight:800;letter-spacing:-.02em}
.dk-head .sp{flex:1}
.dk-eng{display:inline-flex;align-items:center;gap:8px;color:var(--ink2);font-size:13px}
.dk-eng i{width:8px;height:8px;border-radius:50%;background:var(--ok)}
.bd.n{background:var(--sunk);color:var(--ink2)}
.dk-spark{width:70px;height:22px;display:block}
.dk-spark .line{fill:none;stroke:var(--acc);stroke-width:1.6}
.dk-spark .area{fill:var(--acc);opacity:.15}
.dk-img{font-family:var(--mono);font-size:12px;color:var(--ink3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.dk-port{display:inline-flex;font:600 12px var(--mono);padding:2px 7px;border-radius:7px;background:var(--acc-s);color:var(--acc)}
.dk-upd{display:inline-flex;align-items:center;gap:4px;font-size:11.5px;font-weight:800;padding:2px 7px;border-radius:7px;background:var(--h-sw-s);color:var(--h-sw)}
.dk-upd .i{width:12px;height:12px}
.dk-bulk{position:sticky;bottom:16px;align-self:center;display:flex;align-items:center;gap:4px;padding:6px 6px 6px 18px;border-radius:14px;background:var(--ink);color:#000;box-shadow:0 14px 30px -10px rgba(0,0,0,.8)}
.dk-bulk b{margin-right:10px;font-weight:800}
.dk-bulk button{display:flex;align-items:center;gap:6px;padding:8px 12px;border-radius:10px;font-weight:700;font-size:13px;color:#000}
.dk-bulk button:hover{background:rgba(0,0,0,.08)}
.dk-bulk .i{width:16px;height:16px}
.dk-act{display:flex;gap:2px;justify-content:flex-end}
.dk-act button{width:30px;height:30px;border-radius:9px;display:grid;place-items:center;color:var(--ink3)}
.dk-act button:hover{background:var(--surface);color:var(--ink)}
.dk-act .i{width:15px;height:15px}
'''

def badge(state, health):
    t, l = ST[state]
    h = ''
    if health == 'unhealthy': h = '<span class="bd err"><i></i>Unhealthy</span>'
    elif health == 'healthy': h = ''
    return f'<span class="bd {t}"><i></i>{l}</span>{h}'

def spark(seed, live=True):
    return f'<svg class="dk-spark" data-chart="{seed},30,24,1" data-n="30" data-fill="1"></svg>' if live else '<span class="muted">—</span>'

def actions(state):
    first = ('pause','Pause') if state=='running' else ('play','Start')
    return f'<div class="dk-act"><button title="{first[1]}">{I(first[0])}</button><button title="Restart">{I("refresh")}</button><button title="Shell">{I("terminal")}</button><button title="Logs">{I("logs")}</button><button title="More">{I("more")}</button></div>'

def ports(p):
    return f'<span class="dk-port">{p.split(":")[0]}</span>' if p else ''

def head(extra=''):
    return f'''<div class="dk-head"><h1>Docker</h1><span class="dk-eng"><i></i>Engine 28.4.0, 10 running, 1 restarting, 1 stopped, 1 paused</span><div class="sp"></div>{extra}
      <div class="in" style="height:36px;min-width:220px">{I('search')}<input placeholder="Filter containers"></div>
      <button class="b">{I('download')}Pull image</button><button class="b p">{I('plus')}New container</button></div>'''

BULK = f'<div class="dk-bulk"><b>3 selected</b><button>{I("play")}Start</button><button>{I("stop")}Stop</button><button>{I("refresh")}Restart</button><button>{I("download")}Update</button><button>{I("trash")}Remove</button><button>{I("close")}</button></div>'

# ===== A: sub-tabs on top, table grouped by stack =====
def table_rows():
    out=''; last=None
    for i,(stk,n,img,st,h,p,cpu,mem,up,upd) in enumerate(CT):
        if stk != last:
            label = stk or 'Standalone'
            cnt = sum(1 for c in CT if c[0]==stk)
            run = sum(1 for c in CT if c[0]==stk and c[3]=='running')
            acts = f'<span class="sact"><button>{I("refresh")}Restart stack</button><button>{I("edit")}Edit compose</button></span>' if stk else ''
            out += f'<tr class="gh"><td colspan="8"><span class="sn">{I("layers2") if stk else I("box")}{label}</span><span class="muted">{run} of {cnt} running</span>{acts}</td></tr>'
            last = stk
        sel = n in ('nextcloud-app','nextcloud-db','nextcloud-redis')
        out += f'''<tr class="{"sel" if sel else ""}{" bad" if h=="unhealthy" or st=="restarting" else ""}"><td class="dk-cbc"><span class="cbx{" on" if sel else ""}">{I("check") if sel else ""}</span></td>
          <td><div class="nm"><b>{n}</b><span class="dk-img">{img}</span></div></td><td><div class="sts">{badge(st,h)}{'<span class="dk-upd">'+I("download")+'Update</span>' if upd else ''}</div></td>
          <td>{ports(p)}</td><td>{spark(i+3, st=="running")}</td><td class="mono num">{str(cpu)+" %" if st=="running" else "—"}</td><td class="mono num">{mem}</td><td>{actions(st)}</td></tr>'''
    return out
A = fpage('va', COMMON + '''
.dk-tabs{display:flex;gap:4px;flex-wrap:wrap}
.dk-tab{display:inline-flex;align-items:center;gap:8px;height:38px;padding:0 14px;border-radius:12px;font-weight:700;color:var(--ink2)}
.dk-tab .i{width:16px;height:16px}
.dk-tab b{font-size:12px;color:var(--ink3);font-weight:700}
.dk-tab:hover{background:var(--surface)}
.dk-tab.on{background:var(--acc-s);color:var(--acc)}.dk-tab.on b{color:inherit}
.dk-card{background:var(--surface);border-radius:18px;padding:6px 8px 12px;flex:1;display:flex;flex-direction:column;overflow:auto}
table.dk{width:100%;border-collapse:separate;border-spacing:0 3px;font-size:13.5px}
.dk th{text-align:left;font-weight:700;color:var(--ink3);font-size:12.5px;padding:8px 10px}
.dk td{padding:8px 10px;background:var(--sunk)}
.dk tr td:first-child{border-radius:12px 0 0 12px}.dk tr td:last-child{border-radius:0 12px 12px 0}
.dk tr:hover td{background:color-mix(in srgb,var(--sunk) 70%,var(--line))}
.dk tr.sel td{background:var(--acc-s)}
.dk tr.bad td:first-child{box-shadow:inset 3px 0 0 var(--err)}
.dk tr.gh td{background:none;padding:14px 10px 4px}
.dk .sn{display:inline-flex;align-items:center;gap:8px;font-weight:800;font-size:14.5px;margin-right:12px}
.dk .sn .i{width:16px;height:16px;color:var(--acc)}
.dk .sact{float:right;display:flex;gap:4px}
.dk .sact button{display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 10px;border-radius:9px;font-weight:700;font-size:12px;color:var(--ink2)}
.dk .sact button:hover{background:var(--surface);color:var(--ink)}
.dk .sact .i{width:13px;height:13px}
.dk .nm b{display:block;font-weight:700}
.dk .nm{max-width:300px}
.dk .sts{display:flex;gap:6px;flex-wrap:wrap}
.dk .num{text-align:right;white-space:nowrap}
.dk-cbc{width:26px}
.cbx{width:18px;height:18px;border-radius:6px;display:grid;place-items:center;box-shadow:inset 0 0 0 2px var(--line);color:#000}
.cbx.on{background:var(--acc);box-shadow:none}
.cbx .i{width:12px;height:12px}
@media (max-width:1100px){.dk td:nth-child(5),.dk th:nth-child(5),.dk td:nth-child(4),.dk th:nth-child(4){display:none}}
@media (max-width:760px){.dk td:nth-child(6),.dk th:nth-child(6),.dk td:nth-child(7),.dk th:nth-child(7){display:none}}
''', top('Search containers, images, stacks') + f'''
    <section class="dk-wrap">
      {head()}
      <div class="dk-tabs">{''.join(f'<button class="dk-tab{" on" if i==0 else ""}">{I(ic)}{n}{" <b>"+c+"</b>" if c else ""}</button>' for i,(ic,n,c) in enumerate(SUBNAV))}</div>
      <div class="dk-card">
        <table class="dk"><thead><tr><th></th><th>Container</th><th>State</th><th>Port</th><th>Activity</th><th class="num">CPU</th><th class="num">Memory</th><th></th></tr></thead><tbody>{table_rows()}</tbody></table>
        {BULK}
      </div>
    </section>''')

# ===== B: inner sidebar + stack cards with live tiles =====
def ccard(i,c):
    stk,n,img,st,h,p,cpu,mem,up,upd = c
    t,_ = ST[st]
    return f'''<div class="cc {t}{" bad" if h=="unhealthy" or st=="restarting" else ""}">
      <div class="t"><span class="dot"></span><div class="tx"><b>{n}</b><span class="dk-img">{img.split("/")[-1]}</span></div>{'<span class="dk-upd">'+I("download")+'</span>' if upd else ''}</div>
      <div class="m"><div><small>CPU</small><span class="mono">{str(cpu)+"%" if st=="running" else "—"}</span></div><div><small>Memory</small><span class="mono">{mem}</span></div><div><small>Up</small><span>{up}</span></div></div>
      {spark(i+5, st=="running")}
      <div class="f">{ports(p)}<span class="sp"></span>{actions(st)}</div></div>'''
def stacks_cards():
    out=''
    for stk in ['immich','nextcloud','monitoring','']:
        cs=[(i,c) for i,c in enumerate(CT) if c[0]==stk]
        run=sum(1 for _,c in cs if c[3]=='running')
        out += f'''<div class="dstk"><div class="skh"><span class="sn">{I("layers2") if stk else I("box")}{stk or "Standalone"}</span><span class="muted">{run} of {len(cs)} running</span><span class="sp"></span>{'<button class="b g">'+I("edit")+'Compose</button><button class="b">'+I("refresh")+'Restart</button>' if stk else ''}</div>
          <div class="cgrid">{''.join(ccard(i,c) for i,c in cs)}</div></div>'''
    return out
B = fpage('vb', COMMON + '''
.dk-body{flex:1;display:flex;gap:12px;min-height:0}
.dk-nav{width:220px;flex:none;background:var(--surface);border-radius:18px;padding:12px;display:flex;flex-direction:column;gap:2px}
.dk-nav a{display:flex;align-items:center;gap:10px;padding:9px 10px;border-radius:12px;color:var(--ink2);font-weight:700;text-decoration:none}
.dk-nav a .i{width:17px;height:17px}
.dk-nav a b{margin-left:auto;font-size:12px;color:var(--ink3)}
.dk-nav a:hover{background:var(--sunk);color:var(--ink)}
.dk-nav a.on{background:var(--acc-s);color:var(--acc)}.dk-nav a.on b{color:inherit}
.dk-nav .gl{font-size:12px;color:var(--ink3);font-weight:700;margin:12px 10px 4px}
.dk-main{flex:1;min-width:0;display:flex;flex-direction:column;gap:18px}
.dstk{display:flex;flex-direction:column;gap:10px}
.skh{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.skh .sp{flex:1}
.sn{display:inline-flex;align-items:center;gap:8px;font-weight:800;font-size:16px}
.sn .i{width:18px;height:18px;color:var(--acc)}
.cgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(250px,1fr));gap:10px}
.cc{background:var(--surface);border-radius:18px;padding:14px;display:flex;flex-direction:column;gap:10px;position:relative}
.cc.bad{box-shadow:inset 0 0 0 2px var(--err)}
.cc .t{display:flex;align-items:center;gap:10px;min-width:0}
.cc .tx{min-width:0;flex:1;overflow:hidden}.cc .tx .dk-img{display:block}
.cc b{display:block;font-weight:800}
.cc .dot{width:10px;height:10px;border-radius:50%;flex:none;background:var(--ink3)}
.cc.ok .dot{background:var(--ok)}.cc.warn .dot{background:var(--warn)}.cc.info .dot{background:var(--info)}
.cc .m{display:grid;grid-template-columns:1fr 1fr 1.3fr;gap:6px}
.cc .m small{display:block;color:var(--ink3);font-size:11.5px}
.cc .m span{font-size:13px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;display:block}
.cc .dk-spark{width:100%;height:30px}
.cc .f{display:flex;align-items:center;gap:6px}
.cc .f .sp{flex:1}
@media (max-width:1000px){.dk-nav{display:none}}
''', top('Search containers, images, stacks') + f'''
    <section class="dk-wrap">
      {head()}
      <div class="dk-body">
        <nav class="dk-nav"><div class="gl">Workloads</div>{''.join(f'<a class="{"on" if i==0 else ""}">{I(ic)}{n}<b>{c}</b></a>' for i,(ic,n,c) in enumerate(SUBNAV[:2]))}
          <div class="gl">Resources</div>{''.join(f'<a>{I(ic)}{n}<b>{c}</b></a>' for ic,n,c in SUBNAV[2:5])}
          <div class="gl">Tools</div>{''.join(f'<a>{I(ic)}{n}<b>{c}</b></a>' for ic,n,c in SUBNAV[5:])}</nav>
        <div class="dk-main">{stacks_cards()}</div>
      </div>
    </section>''')

# ===== C: health summary first + compact list =====
def clist():
    out=''
    for i,c in enumerate(CT):
        stk,n,img,st,h,p,cpu,mem,up,upd=c
        t,_=ST[st]
        out += f'''<div class="cl {t}{" bad" if h=="unhealthy" or st=="restarting" else ""}"><span class="strip"></span>
          <div class="nm"><b>{n}</b><span class="dk-img">{img}</span></div>
          <span class="stk">{('<span class="pill">'+I("layers2")+stk+'</span>') if stk else ''}</span>
          <div class="sts">{badge(st,h)}{'<span class="dk-upd">'+I("download")+'Update</span>' if upd else ''}</div>
          <span>{ports(p)}</span>{spark(i+7, st=="running")}<span class="mono num">{str(cpu)+"%" if st=="running" else "—"}</span><span class="mono num">{mem}</span><span class="muted up">{up}</span>{actions(st)}</div>'''
    return out
SUM = f'''<div class="sum">
  <div class="sc"><small>Containers</small><b>12</b><span><i class="ok"></i>10 running <i class="warn"></i>1 restarting <i class="n"></i>1 stopped</span></div>
  <div class="sc"><small>CPU, all containers</small><b>20.6%</b><svg class="dk-spark big" data-chart="4,30,20,1" data-n="50" data-fill="1"></svg></div>
  <div class="sc"><small>Memory</small><b>3.2 GB</b><svg class="dk-spark big" data-chart="9,50,6" data-n="50" data-fill="1"></svg></div>
  <div class="sc alert"><small>Needs attention</small><b>3</b><span>vaultwarden unhealthy, nextcloud-redis restart loop, 4 image updates</span></div>
  <div class="sc"><small>Disk used by Docker</small><b>38 GB</b><span>14 GB can be freed <a href="#">Clean up</a></span></div>
</div>'''
C = fpage('vc', COMMON + '''
.sum{display:grid;grid-template-columns:repeat(5,1fr);gap:10px}
.sc{background:var(--surface);border-radius:18px;padding:14px 16px;display:flex;flex-direction:column;gap:4px;min-width:0}
.sc small{color:var(--ink3);font-weight:700;font-size:12px}
.sc b{font-size:26px;font-weight:800;letter-spacing:-.02em}
.sc span{color:var(--ink2);font-size:12.5px}
.sc span i{display:inline-block;width:8px;height:8px;border-radius:50%;margin:0 4px 0 6px}
.sc span i:first-child{margin-left:0}
.sc i.ok{background:var(--ok)}.sc i.warn{background:var(--warn)}.sc i.n{background:var(--ink3)}
.sc a{color:var(--acc);font-weight:700;text-decoration:none;margin-left:6px}
.sc.alert{background:var(--err-s)}.sc.alert b{color:var(--err)}
.dk-spark.big{width:100%;height:28px}
.dk-seg{display:flex;gap:4px;background:var(--surface);border-radius:14px;padding:4px;flex-wrap:wrap;align-self:flex-start}
.dk-seg button{display:inline-flex;align-items:center;gap:7px;height:34px;padding:0 12px;border-radius:10px;font-weight:700;font-size:13px;color:var(--ink3)}
.dk-seg button.on{background:var(--acc);color:#000}
.dk-seg .i{width:15px;height:15px}
.list{background:var(--surface);border-radius:18px;padding:8px;display:flex;flex-direction:column;gap:3px;flex:1;overflow:auto}
.cl{display:grid;grid-template-columns:4px minmax(180px,1.6fr) 120px minmax(150px,1fr) 70px 70px 64px 76px 110px 160px;align-items:center;gap:12px;padding:8px 10px 8px 0;border-radius:12px;background:var(--sunk);font-size:13.5px}
.cl:hover{background:color-mix(in srgb,var(--sunk) 70%,var(--line))}
.cl .strip{align-self:stretch;border-radius:12px 0 0 12px;background:var(--ink3)}
.cl.ok .strip{background:var(--ok)}.cl.warn .strip{background:var(--warn)}.cl.info .strip{background:var(--info)}
.cl.bad{background:var(--err-s)}
.cl .nm{min-width:0;overflow:hidden}.cl .nm .dk-img{display:block}.cl .nm b{display:block;font-weight:700}
.cl .sts{display:flex;gap:6px;flex-wrap:wrap}
.cl .pill{display:inline-flex;align-items:center;gap:5px;font-size:12px;font-weight:700;color:var(--ink2);padding:2px 8px;border-radius:8px;background:var(--surface)}
.cl .pill .i{width:12px;height:12px;color:var(--acc)}
.cl .num{text-align:right}
.cl .up{font-size:12.5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
@media (max-width:1300px){.sum{grid-template-columns:repeat(3,1fr)}.cl{grid-template-columns:4px 1.6fr 1fr 70px 64px 160px}.cl .stk,.cl>span:nth-child(5),.cl .dk-spark,.cl .up{display:none}}
''', top('Search containers, images, stacks') + f'''
    <section class="dk-wrap">
      {head()}
      {SUM}
      <div class="dk-seg">{''.join(f'<button class="{"on" if i==0 else ""}">{I(ic)}{n}</button>' for i,(ic,n,c) in enumerate(SUBNAV))}</div>
      <div class="list">{clist()}</div>
    </section>''')

if __name__ == '__main__':
    write('025-docker-home', {'a': A, 'b': B, 'c': C})
