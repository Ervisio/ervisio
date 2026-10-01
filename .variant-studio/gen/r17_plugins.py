import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *

EXTRA = (('c-file','server','Docker'),('c-svc','shield','Firewall'))
def ppage(cls, css, main, js=''):
    return page(cls, css, main, 'c-plg', js, EXTRA)

# id, name, icon, colour, author, version, update, enabled, desc, verified
PL = [
 ('docker','Docker','server','t-dir','Quadro team','1.4.0','1.5.0',True,'Containers, images, volumes and compose stacks.',True),
 ('ufw','Firewall','shield','t-pdf','Quadro team','0.9.2','',True,'Rules for ufw or nftables, with a simple view and an expert one.',True),
 ('backup','Backups','archive','t-arc','Quadro team','1.1.0','',False,'Scheduled restic or borg backups to disks, SFTP or S3.',True),
 ('net','Network','net','t-code','Quadro team','1.0.3','',True,'Interfaces, Wi-Fi, bridges, VPN and DNS.',True),
 ('disks','Disks','disk','t-txt','Quadro team','0.8.0','',True,'Partitions, SMART health, mounts and LVM.',True),
 ('tailscale','Tailscale','link','t-img','community, @nmoretti','0.3.1','',True,'Join your tailnet and see connected devices.',False),
]
P_CSS = '''
.pi2{width:46px;height:46px;border-radius:15px;display:grid;place-items:center;background:var(--h);color:var(--surface);flex:none}
.pi2 .i{width:22px;height:22px}
.bdg{display:inline-flex;align-items:center;gap:5px;font-size:11.5px;font-weight:800;padding:2px 8px;border-radius:999px;background:var(--s);color:var(--h);white-space:nowrap}
.bdg .i{width:12px;height:12px}
.sw{width:38px;height:22px;border-radius:999px;background:var(--line);position:relative;flex:none;display:inline-block}
.sw::after{content:"";position:absolute;top:3px;left:3px;width:16px;height:16px;border-radius:50%;background:var(--surface)}
.sw.on{background:var(--h-plg)}.sw.on::after{left:19px}
.abtn{display:inline-flex;align-items:center;gap:8px;height:38px;padding:0 14px;border-radius:999px;font-weight:700;font-size:13.5px;background:var(--sunk)}
.abtn:hover{background:var(--line)}
.abtn.p{background:var(--h-plg);color:var(--surface)}
.abtn.dng:hover{background:var(--h-svc-s);color:var(--h-svc)}
.abtn .i{width:16px;height:16px}
.ftabs{display:flex;gap:6px;flex-wrap:wrap}
.ftab{display:inline-flex;align-items:center;gap:7px;height:36px;padding:0 14px;border-radius:999px;font-weight:700;font-size:13.5px;color:var(--ink2)}
.ftab b{font-size:12px;color:var(--ink3)}
.ftab:hover{background:var(--sunk)}
.ftab.on{background:var(--h-plg-s);color:var(--h-plg)}
.ftab.on b{color:inherit}
.fsearch{display:flex;align-items:center;gap:10px;height:38px;padding:0 14px;border-radius:999px;background:var(--sunk);color:var(--ink3);min-width:220px}
.pbar{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid var(--line);flex-wrap:wrap}
.pbar .sp{flex:1}
.perm{display:flex;gap:12px;align-items:flex-start;padding:12px;border-radius:14px;background:var(--sunk)}
.perm .pic{width:32px;height:32px;border-radius:10px;display:grid;place-items:center;background:var(--s);color:var(--h);flex:none}
.perm .pic .i{width:16px;height:16px}
.perm b{display:block;font-weight:700}
.perm small{color:var(--ink3);font-size:12.5px}
.perm code{font:12px var(--mono);color:var(--ink2)}
.muted{color:var(--ink3)}
.h4{margin:0;font-size:13px;color:var(--ink3);font-weight:700}
'''
PERMS = f'''
  <div class="perm t-pdf"><span class="pic">{I('lock')}</span><div><b>Run commands as root</b><small>Only <code>docker</code> and <code>systemctl … docker.service</code>, through the Quadro helper.</small></div></div>
  <div class="perm t-arc"><span class="pic">{I('server')}</span><div><b>Docker socket</b><small><code>/var/run/docker.sock</code>, read and write</small></div></div>
  <div class="perm t-dir"><span class="pic">{I('files')}</span><div><b>Read files</b><small><code>/srv</code>, <code>~/projects</code> to find compose files</small></div></div>
  <div class="perm t-code"><span class="pic">{I('overview')}</span><div><b>Adds to the interface</b><small>A Docker page in the sidebar, 2 dashboard widgets, 3 snippets</small></div></div>'''

def tabs(on='Installed'):
    t = [('Installed','6'),('Updates','1'),('Browse',''),('Developer','')]
    return '<div class="ftabs">' + ''.join(f'<button class="ftab{" on" if n==on else ""}">{n}{" <b>"+c+"</b>" if c else ""}</button>' for n,c in t) + '</div>'

# ===== A: installed cards + detail panel =====
def pcard(pid,n,ic,c,au,v,up,en,d,ver):
    vb = f'<span class="bdg t-code">{I("check")}Verified</span>' if ver else '<span class="bdg t-arc">Community</span>'
    ub = f'<span class="bdg t-plg" style="--h:var(--h-plg);--s:var(--h-plg-s)">Update {up}</span>' if up else ''
    return f'''<div class="pc {c}{" on" if pid=="docker" else ""}{" off" if not en else ""}"><div class="t"><span class="pi2">{I(ic)}</span><div class="tx"><b>{n}</b><small>{au}, v{v}</small></div><span class="sw{" on" if en else ""}"></span></div>
      <p>{d}</p><div class="m">{vb}{ub}</div></div>'''
A = ppage('va', P_CSS + '''
.pg{flex:1;display:flex;min-height:calc(100vh - 90px)}
.pm{flex:1;min-width:0;display:flex;flex-direction:column}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(270px,1fr));gap:12px;padding:16px;align-content:start}
.pc{background:var(--sunk);border-radius:20px;padding:14px;display:flex;flex-direction:column;gap:10px;border:2px solid transparent;cursor:pointer}
.pc.on{border-color:var(--h-plg);background:var(--h-plg-s)}
.pc.off .pi2{background:var(--line);color:var(--ink3)}
.pc .t{display:flex;align-items:center;gap:12px}
.pc .tx{flex:1;min-width:0}
.pc b{display:block;font-weight:800;font-size:15px}
.pc small{color:var(--ink3);font-size:12.5px}
.pc p{margin:0;color:var(--ink2);font-size:13.5px;line-height:1.45}
.pc .m{display:flex;gap:6px;flex-wrap:wrap}
.add{border:2px dashed var(--line);border-radius:20px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:8px;color:var(--ink3);font-weight:700;min-height:150px}
.det{width:400px;flex:none;border-left:1px solid var(--line);padding:18px;display:flex;flex-direction:column;gap:14px;overflow:auto}
.det .dh{display:flex;align-items:center;gap:14px}
.det .dh .pi2{width:56px;height:56px;border-radius:18px}
.det h3{margin:0;font-size:20px;font-weight:800}
.det .acts{display:flex;gap:6px;flex-wrap:wrap}
.upd{display:flex;align-items:center;gap:12px;padding:12px;border-radius:16px;background:var(--h-plg-s)}
.upd b{display:block;font-weight:800;color:var(--h-plg)}
.upd small{color:var(--ink2);font-size:12.5px}
.upd .abtn{margin-left:auto;height:34px}
@media (max-width:1240px){ .det{display:none} }
''', top('Search plugins') + f'''
    <section class="fm pg">
      <div class="pm">
        <div class="pbar">{tabs()}<div class="sp"></div><div class="fsearch">{I('search')}Filter plugins</div><button class="abtn p">{I('plus')}Get plugins</button></div>
        <div class="grid">{''.join(pcard(*p) for p in PL)}<div class="add">{I('plus')}Browse plugins</div></div>
      </div>
      <aside class="det">
        <div class="dh"><span class="pi2 t-dir">{I('server')}</span><div><h3>Docker</h3><small class="muted">Quadro team, v1.4.0</small></div></div>
        <div class="upd"><div><b>Version 1.5.0 available</b><small>Adds compose file editing. Asks for no new permissions.</small></div><button class="abtn p">Update</button></div>
        <div class="acts"><button class="abtn">{I('edit')}Settings</button><button class="abtn">{I('power')}Disable</button><button class="abtn dng">{I('trash')}Uninstall</button></div>
        <p class="h4">What it can do</p>{PERMS}
        <p class="h4">Who can use it</p>
        <div class="perm t-usr" style="--h:var(--h-usr);--s:var(--h-usr-s)"><span class="pic">{I('users')}</span><div><b>Admins and the docker group</b><small>Other users don't see the Docker page.</small></div></div>
      </aside>
    </section>''')

# ===== B: marketplace with consent dialog =====
CAT = [('Containers','server','t-dir'),('Security','shield','t-pdf'),('Networking','net','t-code'),('Storage','disk','t-txt'),('Monitoring','overview','t-doc'),('Web servers','services','t-arc')]
BR = [('Proxmox','cpu','t-arc','Manage VMs and containers on a Proxmox node.','12k','Quadro team',True,False),
      ('Nginx sites','services','t-code','Virtual hosts, certificates and reverse proxies.','31k','Quadro team',True,False),
      ('Fail2ban','shield','t-pdf','Bans, jails and blocked IPs at a glance.','18k','community, @ldv',False,False),
      ('Samba shares','files','t-dir','Share folders with Windows and Mac.','9k','Quadro team',True,False),
      ('Prometheus','overview','t-doc','Export metrics and graph any scrape target.','7k','community, @kv',False,False),
      ('Docker','server','t-dir','Containers, images and compose stacks.','54k','Quadro team',True,True)]
def bcard(n,ic,c,d,dl,au,ver,inst):
    btn = '<span class="ok">Installed</span>' if inst else '<button class="abtn p sm">Install</button>'
    vb = f'<span class="bdg t-code">{I("check")}Verified</span>' if ver else '<span class="bdg t-arc">Community</span>'
    return f'<div class="bc {c}"><span class="pi2">{I(ic)}</span><div class="tx"><b>{n}</b><small>{d}</small><div class="m">{vb}<span class="muted">{dl} installs</span></div></div>{btn}</div>'
B = ppage('vb', P_CSS + '''
.pg{flex:1;min-height:calc(100vh - 90px);display:flex;flex-direction:column;position:relative}
.feat{margin:16px;padding:24px;border-radius:22px;background:var(--h-plg);color:var(--surface);display:flex;align-items:center;gap:20px;flex-wrap:wrap;position:relative;overflow:hidden}
.feat .pi2{width:64px;height:64px;border-radius:20px;background:rgba(255,255,255,.2);color:inherit}
.feat .pi2 .i{width:30px;height:30px}
.feat h2{margin:0;font-size:26px;font-weight:800;letter-spacing:-.02em}
.feat p{margin:4px 0 0;opacity:.9;max-width:46ch}
.feat .abtn{margin-left:auto;background:var(--surface);color:var(--h-plg);height:44px;padding:0 20px}
.cats{display:flex;gap:8px;padding:0 16px 6px;flex-wrap:wrap}
.cat{display:inline-flex;align-items:center;gap:8px;height:38px;padding:0 14px 0 8px;border-radius:999px;background:var(--s);color:var(--h);font-weight:700}
.cat .c{width:26px;height:26px;border-radius:50%;display:grid;place-items:center;background:var(--h);color:var(--surface)}
.cat .c .i{width:14px;height:14px}
.sec{margin:16px 16px 10px;font-weight:800;font-size:16px}
.bgrid{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:10px;padding:0 16px 16px}
.bc{display:flex;align-items:center;gap:12px;padding:12px;border-radius:18px;background:var(--sunk)}
.bc .tx{flex:1;min-width:0}
.bc b{display:block;font-weight:800}
.bc small{display:block;color:var(--ink3);font-size:12.5px}
.bc .m{display:flex;gap:8px;align-items:center;margin-top:6px;font-size:12px}
.bc .ok{font-size:12.5px;font-weight:700;color:var(--ink3)}
.abtn.sm{height:32px;font-size:12.5px}
.scrim{position:absolute;inset:0;background:rgba(10,12,20,.45);display:grid;place-items:center;padding:16px;border-radius:22px}
.dlg{width:min(460px,100%);background:var(--surface);border-radius:24px;padding:22px;display:flex;flex-direction:column;gap:12px;box-shadow:0 30px 80px -20px rgba(0,0,0,.6)}
.dlg .dh{display:flex;align-items:center;gap:14px}
.dlg h3{margin:0;font-size:19px;font-weight:800}
.dlg .lead{margin:0;color:var(--ink2)}
.dlg .r{display:flex;gap:8px;justify-content:flex-end;margin-top:6px}
''', top('Search plugins') + f'''
    <section class="fm pg">
      <div class="pbar">{tabs('Browse')}<div class="sp"></div><div class="fsearch">{I('search')}Search 240 plugins</div></div>
      <div class="feat"><span class="pi2">{I('cpu')}</span><div><h2>Proxmox</h2><p>Start, stop and snapshot your VMs from the same place as the rest of your servers.</p></div><button class="abtn">Install</button></div>
      <div class="cats">{''.join(f'<span class="cat {c}"><span class="c">{I(ic)}</span>{n}</span>' for n,ic,c in CAT)}</div>
      <div class="sec">Popular</div>
      <div class="bgrid">{''.join(bcard(*b) for b in BR)}</div>
      <div class="scrim"><div class="dlg">
        <div class="dh"><span class="pi2 t-code">{I('services')}</span><div><h3>Install Nginx sites?</h3><small class="muted">Quadro team, verified, v2.0.1</small></div></div>
        <p class="lead">This plugin will be able to:</p>
        <div class="perm t-pdf"><span class="pic">{I('lock')}</span><div><b>Run commands as root</b><small><code>nginx -t</code>, <code>systemctl reload nginx</code>, <code>certbot</code></small></div></div>
        <div class="perm t-dir"><span class="pic">{I('files')}</span><div><b>Edit files</b><small><code>/etc/nginx</code>, <code>/etc/letsencrypt</code></small></div></div>
        <div class="perm t-code"><span class="pic">{I('overview')}</span><div><b>Add a page and 1 widget</b><small>Visible to admins only</small></div></div>
        <div class="r"><button class="abtn">Cancel</button><button class="abtn p">Install and allow</button></div>
      </div></div>
    </section>''')

# ===== C: permissions overview + developer =====
CAPS = ['Root commands','Docker socket','Read files','Edit files','Network','Adds pages']
MX = {'docker':[1,1,1,0,0,1],'ufw':[1,0,1,1,1,1],'backup':[1,0,1,0,1,1],'net':[1,0,1,1,1,1],'disks':[1,0,1,0,0,1],'tailscale':[1,0,0,0,1,1]}
rows = ''
for pid,n,ic,c,au,v,up,en,d,ver in PL:
    cells = ''.join(f'<td class="cc"><span class="dot{" on" if x else ""}{" hot" if x and i==0 else ""}"></span></td>' for i,x in enumerate(MX[pid]))
    sig = f'<span class="bdg t-code">{I("check")}Signed</span>' if ver else f'<span class="bdg t-arc">{I("alert")}Unsigned</span>'
    rows += f'<tr class="{c}{" warn" if not ver else ""}"><td><div class="nm"><span class="pi2 sm">{I(ic)}</span><div><b>{n}</b><small>v{v}</small></div></div></td><td>{sig}</td>{cells}<td><span class="sw{" on" if en else ""}"></span></td></tr>'
C = ppage('vc', P_CSS + '''
.pg{flex:1;min-height:calc(100vh - 90px);display:flex;flex-direction:column}
.sum{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:10px;padding:16px}
.sm2{border-radius:18px;padding:14px 16px;background:var(--s);color:var(--h)}
.sm2 .n{font-size:30px;font-weight:800;letter-spacing:-.03em}
.sm2 small{display:block;color:var(--ink2);font-size:13px}
.tb{overflow:auto;padding:0 6px}
table{width:100%;border-collapse:collapse;font-size:13.5px}
th{text-align:left;font-weight:700;color:var(--ink3);font-size:12.5px;padding:10px 12px;border-bottom:1px solid var(--line);white-space:nowrap}
th.cc{text-align:center}
td{padding:9px 12px;border-bottom:1px solid var(--line)}
td.cc{text-align:center}
tr.warn td:first-child{box-shadow:inset 3px 0 0 var(--h-log)}
.nm{display:flex;align-items:center;gap:12px;white-space:nowrap}
.nm b{display:block;font-weight:800}.nm small{color:var(--ink3);font-size:12px}
.pi2.sm{width:36px;height:36px;border-radius:12px}.pi2.sm .i{width:18px;height:18px}
.dot{display:inline-block;width:12px;height:12px;border-radius:50%;border:2px solid var(--line);box-sizing:border-box}
.dot.on{background:var(--h-plg);border-color:var(--h-plg)}
.dot.hot{background:var(--h-svc);border-color:var(--h-svc)}
.dev{margin:16px;padding:16px;border-radius:20px;border:2px dashed var(--line);display:flex;align-items:center;gap:14px;flex-wrap:wrap}
.dev b{display:block;font-weight:800}
.dev small{color:var(--ink3)}
.dev code{font:12.5px var(--mono);padding:2px 8px;border-radius:8px;background:var(--sunk)}
.dev .r{margin-left:auto;display:flex;gap:8px}
''', top('Search plugins') + f'''
    <section class="fm pg">
      <div class="pbar">{tabs()}<div class="sp"></div><div class="fsearch">{I('search')}Filter plugins</div><button class="abtn p">{I('plus')}Get plugins</button></div>
      <div class="sum">
        <div class="sm2 t-plg" style="--h:var(--h-plg);--s:var(--h-plg-s)"><span class="n">6</span><small>plugins installed, 5 enabled</small></div>
        <div class="sm2 t-pdf"><span class="n">6</span><small>can run root commands</small></div>
        <div class="sm2 t-arc"><span class="n">1</span><small>unsigned, from the community</small></div>
        <div class="sm2 t-code"><span class="n">1</span><small>update ready</small></div>
      </div>
      <div class="tb"><table><thead><tr><th>Plugin</th><th>Signature</th>{''.join(f'<th class="cc">{c}</th>' for c in CAPS)}<th>On</th></tr></thead><tbody>{rows}</tbody></table></div>
      <div class="dev"><span class="pi2 t-code">{I('code')}</span><div><b>Developer mode</b><small>Load a plugin from a folder and reload it on save, e.g. <code>~/projects/quadro-plugin-zfs</code></small></div><div class="r"><button class="abtn">{I('files')}Load from folder</button><button class="abtn">{I('logs')}Plugin docs</button></div></div>
    </section>''')

if __name__ == '__main__':
    write('017-plugins', {'a': A, 'b': B, 'c': C})
