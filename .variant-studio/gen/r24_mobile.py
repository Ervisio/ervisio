import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *
import r23_components as K

SEC = [('c-ov','overview','Overview'),('c-term','terminal','Terminal'),('c-file','files','Files'),('c-log','logs','Logs'),
       ('c-svc','services','Services'),('c-sw','software','Software'),('c-usr','users','Users'),('c-plg','plugins','Plugins')]

CSS = K.OLED.replace('display:block;padding:28px;min-height:100vh;box-sizing:border-box','display:block;padding:24px;min-height:100vh;box-sizing:border-box') + K.BASE + K.STYLES['b'][1] + '''
.k .c-ov{--h:var(--h-ov)}.k .c-term{--h:var(--h-term)}.k .c-file{--h:var(--h-file)}.k .c-log{--h:var(--h-log)}.k .c-svc{--h:var(--h-svc)}.k .c-sw{--h:var(--h-sw)}.k .c-usr{--h:var(--h-usr)}.k .c-plg{--h:var(--h-plg)}
.k [class*="c-"]{--s:color-mix(in srgb,var(--h) 18%,#000)}
.phones{display:flex;gap:26px;flex-wrap:wrap;justify-content:center}
.ph{width:360px;flex:none}
.ph .cap{text-align:center;color:var(--ink2);font-weight:700;font-size:13.5px;margin-bottom:10px}
.scr{width:360px;height:740px;border-radius:40px;background:var(--bg);box-shadow:0 0 0 8px #1b1b1e,0 30px 60px -20px rgba(0,0,0,.8);position:relative;overflow:hidden;display:flex;flex-direction:column}
.sb{height:34px;display:flex;align-items:center;justify-content:space-between;padding:0 26px;font-size:13px;font-weight:700;flex:none}
.ap{flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0}
.hdr{display:flex;align-items:center;gap:10px;padding:8px 14px 10px}
.hdr h2{margin:0;font-size:22px;font-weight:800;flex:1}
.hdr .ib2{width:38px;height:38px;border-radius:12px;background:var(--surface);display:grid;place-items:center;color:var(--ink2)}
.chips{display:flex;gap:8px;padding:0 14px 10px;overflow:hidden}
.chip{flex:none;height:32px;padding:0 12px;border-radius:10px;background:var(--surface);display:inline-flex;align-items:center;gap:6px;font-weight:700;font-size:13px;color:var(--ink2)}
.chip.on{background:var(--h-svc);color:#000}
.ban{margin:0 14px 10px;padding:10px 12px;border-radius:14px;background:var(--err-s);color:var(--err);font-size:13px;font-weight:700;display:flex;gap:8px;align-items:center}
.list{padding:0 14px;display:flex;flex-direction:column;gap:6px;overflow:hidden;flex:1}
.lr{display:flex;align-items:center;gap:12px;padding:12px;border-radius:16px;background:var(--surface)}
.lr .tx{flex:1;min-width:0}
.lr b{display:block;font-weight:700;font-size:14.5px}
.lr small{color:var(--ink3);font-size:12.5px}
.lr .sw{transform:scale(.85)}
.dot{width:10px;height:10px;border-radius:50%;flex:none}
/* sheet */
.scrim{position:absolute;inset:0;background:rgba(0,0,0,.6)}
.sheet{position:absolute;left:0;right:0;bottom:0;background:#121214;border-radius:26px 26px 0 0;padding:8px 16px 20px;display:flex;flex-direction:column;gap:12px;max-height:82%}
.grab{width:40px;height:5px;border-radius:3px;background:var(--line);margin:4px auto 4px}
.sheet .hd{display:flex;align-items:center;gap:12px}
.sheet .hd .ic{width:44px;height:44px;border-radius:14px;display:grid;place-items:center;background:var(--s);color:var(--h)}
.sheet h3{margin:0;font-size:19px;font-weight:800}
.sheet .acts{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}
.sheet .acts .b{flex-direction:column;height:64px;gap:4px;font-size:12.5px}
.tabs2{display:flex;gap:4px;background:var(--sunk);border-radius:12px;padding:3px}
.tabs2 button{flex:1;height:32px;border-radius:9px;font-weight:700;font-size:12.5px;color:var(--ink3)}
.tabs2 button.on{background:var(--surface);color:var(--ink)}
.kv2{display:grid;grid-template-columns:auto 1fr;gap:8px 14px;font-size:13.5px;margin:0}
.kv2 dt{color:var(--ink3)}.kv2 dd{margin:0;font-weight:600}
/* terminal */
.term{flex:1;background:#000;font:12.5px/1.6 var(--mono);color:#E4E4E7;padding:10px 12px;white-space:pre-wrap;overflow:hidden}
.term .g{color:var(--h-term)}.term .bl{color:var(--h-file)}.term .d{color:var(--ink3)}
.keys{display:flex;gap:6px;padding:8px;background:var(--surface);overflow:hidden}
.keys span{flex:none;min-width:40px;height:36px;padding:0 8px;border-radius:10px;background:var(--sunk);display:grid;place-items:center;font:700 12.5px var(--mono);color:var(--ink2)}
.keys span.on{background:var(--h-term);color:#000}
.kb{height:190px;background:#1a1a1d;display:grid;grid-template-rows:repeat(4,1fr);gap:6px;padding:8px 4px}
.kb div{display:flex;gap:5px;justify-content:center}
.kb i{flex:1;max-width:30px;border-radius:6px;background:#2c2c31}
.tstrip{display:flex;gap:6px;padding:8px 10px;background:var(--surface);align-items:center}
.tstrip .t{display:flex;align-items:center;gap:6px;height:30px;padding:0 10px;border-radius:9px;background:var(--sunk);font-weight:700;font-size:12px}
.tstrip .t.on{background:var(--h-term);color:#000}
/* nav A: dock + more sheet */
.dock{display:flex;justify-content:space-around;padding:8px 6px 18px;background:var(--surface);flex:none}
.dock a{display:flex;flex-direction:column;align-items:center;gap:3px;font-size:11px;font-weight:700;color:var(--ink3);text-decoration:none;width:64px}
.dock a .pi{width:52px;height:30px;border-radius:12px;display:grid;place-items:center}
.dock a.on{color:var(--h)}
.dock a.on .pi{background:var(--s);color:var(--h)}
.dock a .i{width:20px;height:20px}
.mgrid{display:grid;grid-template-columns:repeat(4,1fr);gap:10px}
.mg{display:flex;flex-direction:column;align-items:center;gap:6px;font-size:12px;font-weight:700;color:var(--ink2)}
.mg .t{width:56px;height:56px;border-radius:18px;display:grid;place-items:center;background:var(--s);color:var(--h)}
.mg .t .i{width:24px;height:24px}
.hostsw{display:flex;align-items:center;gap:10px;padding:12px;border-radius:14px;background:var(--surface)}
.hostsw b{font-weight:800}.hostsw small{color:var(--ink3);font-size:12px;display:block}
/* nav B: dock with centre search */
.dock .fab{width:56px;height:56px;border-radius:18px;background:var(--acc);color:#000;display:grid;place-items:center;margin-top:-22px;box-shadow:0 10px 24px -8px rgba(0,0,0,.8)}
.dock .fab .i{width:24px;height:24px}
.pal{position:absolute;inset:0;background:var(--bg);display:flex;flex-direction:column}
.pal .in{margin:10px 14px}
.pal .grp{font-size:12px;color:var(--ink3);font-weight:700;padding:12px 18px 6px}
.pal .it{display:flex;align-items:center;gap:12px;padding:10px 18px}
.pal .it .t{width:36px;height:36px;border-radius:12px;display:grid;place-items:center;background:var(--s);color:var(--h)}
.pal .it b{font-weight:700;display:block}.pal .it small{color:var(--ink3);font-size:12px}
/* nav C: launcher */
.launch{flex:1;padding:6px 14px;display:grid;grid-template-columns:1fr 1fr;gap:10px;align-content:start}
.ln{border-radius:22px;padding:14px;background:var(--s);color:var(--h);display:flex;flex-direction:column;gap:18px;height:104px;box-sizing:border-box}
.ln .i{width:26px;height:26px}
.ln b{font-weight:800;font-size:15px;color:var(--ink)}
.ln small{display:block;font-size:12px;color:var(--ink2);font-weight:600}
.back{display:flex;align-items:center;gap:6px;color:var(--ink2);font-weight:700;font-size:14px}
'''

def status():
    return '<div class="sb"><span>17:45</span><span>5G  86%</span></div>'

SVC = [('nginx','Running, 14 MB','var(--ok)',True),('docker','Running, 182 MB','var(--ok)',True),('postgresql','Running, 96 MB','var(--ok)',True),
       ('sshd','Running','var(--ok)',True),('bluetooth','Stopped','var(--ink3)',False),('cups','Stopped','var(--ink3)',False)]
def services_list():
    rows = ''.join(f'<div class="lr"><span class="dot" style="background:{c}"></span><div class="tx"><b>{n}</b><small>{d}</small></div><span class="sw{" on" if on else ""}"></span></div>' for n,d,c,on in SVC)
    return f'''<div class="hdr"><h2>Services</h2><span class="ib2">{I('search')}</span><span class="ib2">{I('filter')}</span></div>
      <div class="chips"><span class="chip on">All 142</span><span class="chip">Failed 1</span><span class="chip">Web</span><span class="chip">Containers</span><span class="chip">System</span></div>
      <div class="ban">{I('alert')}networkd-wait-online failed</div>
      <div class="list">{rows}</div>'''

def sheet():
    return f'''<div class="scrim"></div><div class="sheet c-file">
      <div class="grab"></div>
      <div class="hd"><span class="ic">{I('services')}</span><div><h3>nginx</h3><span class="bd ok"><i></i>Running for 6 min</span></div></div>
      <div class="acts"><button class="b">{I('refresh')}Restart</button><button class="b">{I('play')}Reload</button><button class="b">{I('power')}Stop</button></div>
      <div class="tabs2"><button class="on">Info</button><button>Logs</button><button>Unit</button><button>Deps</button></div>
      <dl class="kv2"><dt>Start at boot</dt><dd><span class="sw on" style="display:inline-block;vertical-align:middle;transform:scale(.85)"></span></dd><dt>Memory</dt><dd>14.2 MB</dd><dt>Main PID</dt><dd>81244</dd><dt>Group</dt><dd>Web</dd></dl>
    </div>'''

def terminal(extra_top=''):
    return f'''{extra_top}<div class="tstrip"><span class="t on">fonlogen@arch</span><span class="t">root</span><span class="t">{I('plus')}</span></div>
      <div class="term"><span class="g">fonlogen@arch</span> <span class="bl">~</span> $ systemctl status nginx
<span class="g">●</span> nginx.service - web server
   Active: <span class="g">active (running)</span>
   Memory: 14.2M

<span class="g">fonlogen@arch</span> <span class="bl">~</span> $ docker ps
web       Up 3 hours
postgres  Up 3 hours
<span class="g">fonlogen@arch</span> <span class="bl">~</span> $ sudo pacman -S<span style="background:#E4E4E7;color:#000"> </span></div>
      <div class="keys"><span class="on">Ctrl</span><span>Esc</span><span>Tab</span><span>↑</span><span>↓</span><span>←</span><span>→</span><span>|</span><span>~</span></div>
      <div class="kb"><div>{"<i></i>"*10}</div><div>{"<i></i>"*9}</div><div>{"<i></i>"*9}</div><div><i style="max-width:60px"></i><i style="max-width:160px"></i><i style="max-width:60px"></i></div></div>'''

def dockA(active):
    items = [('c-ov','overview','Home'),('c-term','terminal','Terminal'),('c-file','files','Files'),('c-svc','services','Services')]
    out = ''.join(f'<a class="{c}{" on" if c==active else ""}"><span class="pi">{I(ic)}</span>{l}</a>' for c,ic,l in items)
    return f'<nav class="dock">{out}<a class="{"on c-ov" if active=="more" else ""}"><span class="pi">{I("more")}</span>More</a></nav>'
def dockB(active):
    items = [('c-ov','overview','Home'),('c-svc','services','Services')]
    items2 = [('c-term','terminal','Terminal'),('more','grid','All')]
    a = ''.join(f'<a class="{c}{" on" if c==active else ""}"><span class="pi">{I(ic)}</span>{l}</a>' for c,ic,l in items)
    b = ''.join(f'<a class="{c}{" on" if c==active else ""}"><span class="pi">{I(ic)}</span>{l}</a>' for c,ic,l in items2)
    return f'<nav class="dock">{a}<span class="fab">{I("search")}</span>{b}</nav>'

def phone(cap, inner):
    return f'<div class="ph"><div class="cap">{cap}</div><div class="scr">{status()}<div class="ap">{inner}</div></div></div>'

MORE_GRID = ''.join(f'<div class="mg {c}"><span class="t">{I(ic)}</span>{l}</div>' for c,ic,l in SEC) + \
            f'<div class="mg c-file"><span class="t">{I("server")}</span>Docker</div><div class="mg c-svc"><span class="t">{I("shield")}</span>Firewall</div>' + \
            f'<div class="mg" style="--h:var(--ink2);--s:var(--sunk)"><span class="t">{I("cog")}</span>Settings</div>'

def variant(nav):
    if nav == 'a':
        p1 = phone('Services list', services_list() + dockA('c-svc'))
        p2 = phone('Details as a bottom sheet', services_list() + dockA('c-svc') + sheet())
        p3 = phone('More: everything else', services_list() + dockA('more') + f'''<div class="scrim"></div><div class="sheet"><div class="grab"></div>
           <div class="hostsw"><span class="dot" style="background:var(--ok)"></span><div><b>arch</b><small>192.168.1.137</small></div><span style="margin-left:auto;color:var(--ink3)">{I('chevron')}</span></div>
           <div class="mgrid">{MORE_GRID}</div></div>''')
        p4 = phone('Terminal with extra keys', terminal())
    elif nav == 'b':
        p1 = phone('Services list', services_list() + dockB('c-svc'))
        p2 = phone('Details as a bottom sheet', services_list() + dockB('c-svc') + sheet())
        p3 = phone('Centre button: search and jump', services_list() + dockB('c-svc') + f'''<div class="pal">
           <div class="in">{I('search')}<input value="ngi"></div>
           <div class="grp">Services</div><div class="it c-svc"><span class="t">{I('services')}</span><div><b>nginx</b><small>Running, restart or open logs</small></div></div>
           <div class="grp">Files</div><div class="it c-file"><span class="t">{I('files')}</span><div><b>/etc/nginx</b><small>Folder</small></div></div>
           <div class="grp">Go to</div>{''.join(f'<div class="it {c}"><span class="t">{I(ic)}</span><div><b>{l}</b></div></div>' for c,ic,l in SEC[:3])}</div>''')
        p4 = phone('Terminal with extra keys', terminal())
    else:
        hdr = f'<div class="hdr"><span class="ib2">{I("menu")}</span><h2 style="font-size:18px">Services</h2><span class="ib2">{I("search")}</span></div>'
        p1 = phone('Services list, menu top left', services_list().replace('<div class="hdr"><h2>Services</h2><span class="ib2">'+I('search')+'</span><span class="ib2">'+I('filter')+'</span></div>', hdr))
        p2 = phone('Details as a bottom sheet', services_list().replace('<div class="hdr"><h2>Services</h2><span class="ib2">'+I('search')+'</span><span class="ib2">'+I('filter')+'</span></div>', hdr) + sheet())
        launch = ''.join(f'<div class="ln {c}">{I(ic)}<div><b>{l}</b><small>{s}</small></div></div>' for (c,ic,l),s in zip(SEC,['All good','3 sessions','~','2 errors today','1 failed','23 updates','5 people','6 installed']))
        p3 = phone('Menu: a launcher of sections', f'''<div class="hdr"><span class="ib2">{I('close')}</span><h2 style="font-size:18px">arch</h2><span class="ib2">{I('cog')}</span></div>
           <div class="launch">{launch}</div>''')
        p4 = phone('Terminal with extra keys', terminal(f'<div class="hdr"><span class="ib2">{I("menu")}</span><h2 style="font-size:18px">Terminal</h2><span class="ib2">{I("columns")}</span></div>'))
    return f'''{FONT}
<style>{CSS}</style>
<div class="q k"><div class="phones">{p1}{p2}{p3}{p4}</div></div>
<script src="kit.js"></script>'''

write('024-mobile', {k: variant(k) for k in 'abc'})
