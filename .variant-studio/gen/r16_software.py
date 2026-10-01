import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *

def wpage(cls, css, main, js=''):
    return page(cls, css, main, 'c-sw', js)

SRCB = {'core':('core','t-txt'),'extra':('extra','t-txt'),'aur':('AUR','t-arc'),'flatpak':('Flatpak','t-dir')}
# name, from, to, source, size, note
UPD = [
 ('linux','7.2.6.arch2-1','7.2.7.arch1-1','core','142 MB','Reboot needed'),
 ('systemd','259.2-1','259.3-1','core','9.8 MB',''),
 ('mesa','26.2.3-1','26.2.4-1','extra','11.4 MB',''),
 ('firefox','143.0.1-1','143.0.2-1','extra','71 MB','Restart Firefox'),
 ('docker','1:28.4.0-1','1:28.4.1-1','extra','29 MB','Restarts docker.service'),
 ('openssl','3.5.3-1','3.5.4-1','core','6.2 MB','Security fix'),
 ('visual-studio-code-bin','1.104.2-1','1.105.0-1','aur','118 MB','Builds from AUR'),
 ('com.spotify.Client','1.2.70','1.2.72','flatpak','182 MB',''),
]
SW_CSS = '''
.src{display:inline-flex;font-size:11.5px;font-weight:800;padding:2px 8px;border-radius:7px;background:var(--s);color:var(--h);white-space:nowrap}
.t-txt.src{background:var(--h-sw-s);color:var(--h-sw)}
.ver{font:12.5px var(--mono);color:var(--ink3);white-space:nowrap}
.ver b{color:var(--ink);font-weight:600}
.ver .ar{margin:0 6px;color:var(--ink3)}
.note{font-size:12px;font-weight:700;padding:2px 8px;border-radius:7px;white-space:nowrap}
.note.r{background:var(--h-log-s);color:var(--h-log)}
.note.s{background:var(--h-svc-s);color:var(--h-svc)}
.note.n{color:var(--ink3);padding:0}
.abtn{display:inline-flex;align-items:center;gap:8px;height:38px;padding:0 14px;border-radius:999px;font-weight:700;font-size:13.5px;background:var(--sunk)}
.abtn:hover{background:var(--line)}
.abtn.p{background:var(--h-sw);color:var(--surface)}
.abtn .i{width:16px;height:16px}
.ftabs{display:flex;gap:6px;flex-wrap:wrap}
.ftab{display:inline-flex;align-items:center;gap:7px;height:36px;padding:0 14px;border-radius:999px;font-weight:700;font-size:13.5px;color:var(--ink2)}
.ftab b{font-size:12px;color:var(--ink3)}
.ftab:hover{background:var(--sunk)}
.ftab.on{background:var(--h-sw-s);color:var(--h-sw)}
.ftab.on b{color:inherit}
.fsearch{display:flex;align-items:center;gap:10px;height:38px;padding:0 14px;border-radius:999px;background:var(--sunk);color:var(--ink3);min-width:220px}
.sbar{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid var(--line);flex-wrap:wrap}
.sbar .sp{flex:1}
.ck{width:20px;height:20px;border-radius:7px;display:inline-grid;place-items:center;background:var(--h-sw);color:var(--surface);flex:none}
.ck .i{width:13px;height:13px}
.ck.off{background:none;border:2px solid var(--line);box-sizing:border-box}
'''
def note(n):
    if not n: return ''
    cls = 's' if 'Security' in n else ('r' if ('Reboot' in n or 'Restart' in n) else 'n')
    return f'<span class="note {cls}">{n}</span>'

TABS = lambda: f'<div class="ftabs"><button class="ftab on">Updates <b>23</b></button><button class="ftab">Installed <b>883</b></button><button class="ftab">Find software</button><button class="ftab">History</button></div>'

# ===== A: updates first =====
def upd_rows(group):
    out=''
    for n,f,t,s,sz,nt in UPD:
        if (group=='repo' and s in ('core','extra')) or s==group:
            l,c = SRCB[s]
            out += f'<div class="ur"><span class="ck{" off" if s=="aur" else ""}">{I("check") if s!="aur" else ""}</span><div class="nm"><b>{n}</b><span class="ver">{f}<span class="ar">→</span><b>{t}</b></span></div><span class="src {c}">{l}</span>{note(nt)}<span class="sz">{sz}</span></div>'
    return out
A = wpage('va', SW_CSS + '''
.sw{flex:1;min-height:calc(100vh - 90px);display:flex;flex-direction:column}
.hero{display:flex;align-items:center;gap:20px;margin:16px;padding:22px 24px;border-radius:22px;background:var(--h-sw-s);flex-wrap:wrap}
.hero .n{font-size:56px;font-weight:800;letter-spacing:-.05em;line-height:1;color:var(--h-sw)}
.hero b{display:block;font-size:18px;font-weight:800}
.hero small{color:var(--ink2);font-size:13.5px}
.hero .r{margin-left:auto;display:flex;gap:8px;flex-wrap:wrap}
.hero .abtn{height:44px;padding:0 20px;font-size:14.5px}
.hero .abtn:not(.p){background:var(--surface)}
.list{padding:0 16px 20px;overflow:auto}
.gh{display:flex;align-items:center;gap:10px;margin:14px 4px 8px;font-weight:800}
.gh .muted{font-weight:600;font-size:13px}
.gh button{margin-left:auto;font-weight:700;font-size:13px;color:var(--h-sw)}
.ur{display:grid;grid-template-columns:auto 1fr auto auto 80px;align-items:center;gap:14px;padding:11px 14px;border-radius:14px;background:var(--sunk);margin-bottom:6px}
.ur .nm{min-width:0}
.ur .nm b{display:block;font-weight:800}
.ur .sz{color:var(--ink3);font-size:12.5px;text-align:right}
.more{display:block;width:100%;padding:10px;border-radius:14px;font-weight:700;color:var(--h-sw);text-align:center}
.aurw{display:flex;gap:10px;align-items:flex-start;padding:10px 12px;border-radius:14px;background:var(--h-log-s);color:var(--h-log);font-size:13px;font-weight:600;margin:0 0 8px}
@media (max-width:860px){ .ur{grid-template-columns:auto 1fr auto} .ur .note,.ur .sz{display:none} }
''', top('Search packages') + f'''
    <section class="fm sw">
      <div class="sbar">{TABS()}<div class="sp"></div><span class="muted" style="font-size:13px">Checked 4 min ago</span><button class="abtn">{I('refresh')}Check now</button></div>
      <div class="hero"><span class="n">23</span><div><b>Updates ready</b><small>412 MB to download. linux 7.2.7 needs a reboot, openssl has a security fix.</small></div>
        <div class="r"><button class="abtn">{I('clock')}Tonight at 03:00</button><button class="abtn p">{I('download')}Update all</button></div></div>
      <div class="list">
        <div class="gh">Official repositories <span class="muted">18</span><button>Select none</button></div>{upd_rows('repo')}<button class="more">Show 12 more</button>
        <div class="gh">AUR <span class="muted">1</span></div>
        <div class="aurw">{I('alert')}<div>AUR packages are built from community scripts. Review the PKGBUILD before updating, they are left unselected.</div></div>{upd_rows('aur')}
        <div class="gh">Flatpak <span class="muted">1</span></div>{upd_rows('flatpak')}
      </div>
    </section>''')

# ===== B: app store =====
APPS = [('Firefox','t-arc','Web browser','extra','143.0.1',True),('Visual Studio Code','t-dir','Code editor','aur','1.104.2',True),
        ('Spotify','t-code','Music','flatpak','1.2.70',True),('Docker','t-doc','Containers','extra','28.4.0',True),
        ('GIMP','t-txt','Image editor','extra','3.0.4',False),('Thunderbird','t-dir','Email','flatpak','140.3',False),
        ('OBS Studio','t-pdf','Recording','extra','31.1',False),('Blender','t-arc','3D','flatpak','4.5',False)]
def app(n,c,d,s,v,inst):
    l,sc = SRCB[s]
    btn = '<span class="upd">Update</span>' if inst and n in ('Firefox','Visual Studio Code','Spotify','Docker') else ('<span class="ok">Installed</span>' if inst else '<button class="abtn p sm">Install</button>')
    return f'<div class="ap {c}"><span class="ic">{n[0]}</span><div class="tx"><b>{n}</b><small>{d}</small><div class="m"><span class="src {sc}">{l}</span><span class="ver">{v}</span></div></div>{btn}</div>'
CATS = [('Development','code','t-code'),('Internet','net','t-dir'),('Graphics','image','t-img'),('Media','music','t-pdf'),('System','server','t-doc'),('Servers','services','t-arc')]
B = wpage('vb', SW_CSS + '''
.sw{flex:1;min-height:calc(100vh - 90px);display:flex;flex-direction:column}
.big{margin:18px 16px 6px;display:flex;align-items:center;gap:14px;height:56px;padding:0 22px;border-radius:999px;background:var(--sunk);font-size:16px;color:var(--ink3)}
.big .i{width:22px;height:22px}
.big .srcs{margin-left:auto;display:flex;gap:6px}
.cats{display:flex;gap:8px;padding:10px 16px;flex-wrap:wrap}
.cat{display:inline-flex;align-items:center;gap:8px;height:40px;padding:0 16px 0 8px;border-radius:999px;background:var(--s);color:var(--h);font-weight:700}
.cat .c{width:28px;height:28px;border-radius:50%;display:grid;place-items:center;background:var(--h);color:var(--surface)}
.cat .c .i{width:15px;height:15px}
.sec{display:flex;align-items:center;gap:10px;margin:16px 16px 10px;font-weight:800;font-size:16px}
.sec .muted{font-weight:600;font-size:13px}
.sec button{margin-left:auto;font-weight:700;font-size:13px;color:var(--h-sw)}
.apps{display:grid;grid-template-columns:repeat(auto-fill,minmax(270px,1fr));gap:10px;padding:0 16px}
.ap{display:flex;align-items:center;gap:12px;padding:12px;border-radius:18px;background:var(--sunk)}
.ap .ic{width:52px;height:52px;border-radius:16px;display:grid;place-items:center;font-size:22px;font-weight:800;background:var(--h);color:var(--surface);flex:none}
.ap .tx{flex:1;min-width:0}
.ap b{display:block;font-weight:800}
.ap small{color:var(--ink3);font-size:12.5px}
.ap .m{display:flex;gap:8px;align-items:center;margin-top:4px}
.ap .upd{font-size:12.5px;font-weight:800;padding:6px 12px;border-radius:999px;background:var(--h-sw);color:var(--surface)}
.ap .ok{font-size:12.5px;font-weight:700;color:var(--ink3)}
.abtn.sm{height:32px;font-size:12.5px}
.pk{margin:20px 16px;padding:14px 16px;border-radius:18px;border:1px solid var(--line);display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.pk b{font-weight:800}
.pk small{color:var(--ink3)}
.pk .abtn{margin-left:auto}
''', top('Search packages') + f'''
    <section class="fm sw">
      <div class="sbar">{TABS().replace('Find software','Find software').replace('class="ftab on">Updates','class="ftab">Updates').replace('<button class="ftab">Find software','<button class="ftab on">Find software')}</div>
      <div class="big">{I('search')}Search apps and packages<div class="srcs"><span class="src t-txt">Repos</span><span class="src t-arc">AUR</span><span class="src t-dir">Flatpak</span></div></div>
      <div class="cats">{''.join(f'<span class="cat {c}"><span class="c">{I(ic)}</span>{n}</span>' for n,ic,c in CATS)}</div>
      <div class="sec">Your apps <span class="muted">4 with updates</span><button>Update all</button></div>
      <div class="apps">{''.join(app(*a) for a in APPS[:4])}</div>
      <div class="sec">Popular on Arch</div>
      <div class="apps">{''.join(app(*a) for a in APPS[4:])}</div>
      <div class="pk"><span class="ft-ic t-sw" style="--h:var(--h-sw);--s:var(--h-sw-s)">{I('software')}</span><div><b>883 packages installed</b><br><small>76 installed by you, the rest are dependencies. Apps above are the ones with a desktop entry.</small></div><button class="abtn">{I('list')}All packages</button></div>
    </section>''')

# ===== C: installed table + live transaction panel =====
PK = [('base','3-2','core','1 KB','Explicit','12 Mar 2025'),('bash','5.3.3-2','core','9.4 MB','Dependency','2 Sep'),
      ('docker','1:28.4.0-1','extra','108 MB','Explicit','14 Sep'),('firefox','143.0.1-1','extra','262 MB','Explicit','24 Sep'),
      ('linux','7.2.6.arch2-1','core','139 MB','Explicit','28 Sep'),('nginx','1.29.1-1','extra','3.4 MB','Explicit','2 Aug'),
      ('openssl','3.5.3-1','core','10.1 MB','Dependency','10 Sep'),('postgresql','17.6-1','extra','58 MB','Explicit','2 Aug'),
      ('visual-studio-code-bin','1.104.2-1','aur','412 MB','Explicit','20 Sep'),('yay','12.5.0-1','aur','9 MB','Explicit','12 Mar 2025')]
trs = ''.join(f'<tr{" class=\"sel\"" if n=="docker" else ""}><td><b>{n}</b></td><td class="ver">{v}</td><td><span class="src {SRCB[s][1]}">{SRCB[s][0]}</span></td><td class="muted">{sz}</td><td class="{"muted" if r=="Dependency" else ""}">{r}</td><td class="muted">{d}</td></tr>' for n,v,s,sz,r,d in PK)
TX = '''<span class="d">:: Synchronizing package databases...</span>
 core is up to date
 extra is up to date
<span class="d">:: Starting full system upgrade...</span>
<span class="g">(1/18)</span> upgrading systemd           <span class="ok">done</span>
<span class="g">(2/18)</span> upgrading openssl           <span class="ok">done</span>
<span class="g">(3/18)</span> upgrading mesa              <span class="ok">done</span>
<span class="g">(4/18)</span> upgrading linux             <span class="y">62%</span>'''
C = wpage('vc', SW_CSS + '''
.sw{flex:1;display:flex;min-height:calc(100vh - 90px)}
.swm{flex:1;min-width:0;display:flex;flex-direction:column}
.chips{display:flex;gap:6px;padding:10px 16px;border-bottom:1px solid var(--line);flex-wrap:wrap}
.chips span{display:inline-flex;align-items:center;gap:6px;height:32px;padding:0 12px;border-radius:999px;background:var(--sunk);font-weight:700;font-size:13px;color:var(--ink2)}
.chips span.on{background:var(--h-sw-s);color:var(--h-sw)}
.tb{flex:1;overflow:auto}
table{width:100%;border-collapse:collapse;font-size:13.5px}
th{position:sticky;top:0;background:var(--surface);text-align:left;font-weight:700;color:var(--ink3);font-size:12.5px;padding:10px 14px;border-bottom:1px solid var(--line)}
td{padding:9px 14px;border-bottom:1px solid var(--line);white-space:nowrap}
td b{font-weight:700}
tr:hover td{background:var(--sunk)}
tr.sel td{background:var(--h-sw-s)}
.tx{width:400px;flex:none;border-left:1px solid var(--line);display:flex;flex-direction:column}
.tx .hd{padding:16px 18px;display:flex;flex-direction:column;gap:10px;border-bottom:1px solid var(--line)}
.tx .hd b{font-size:16px;font-weight:800}
.tx .bar{height:8px;border-radius:4px;background:var(--sunk);overflow:hidden}
.tx .bar i{display:block;height:100%;width:22%;background:var(--h-sw);border-radius:4px}
.tx .row{display:flex;justify-content:space-between;color:var(--ink3);font-size:12.5px}
.tx pre{margin:0;flex:1;padding:14px 18px;font:12px/1.75 var(--mono);color:var(--ink2);overflow:auto;background:var(--sunk)}
.tx pre .d{color:var(--ink3)}.tx pre .g{color:var(--h-sw)}.tx pre .ok{color:var(--h-term)}.tx pre .y{color:var(--h-log);font-weight:700}
.tx .ft{padding:14px 18px;display:flex;flex-direction:column;gap:10px}
.tx .warn{display:flex;gap:10px;padding:10px 12px;border-radius:14px;background:var(--h-log-s);color:var(--h-log);font-size:13px;font-weight:600}
.tx .warn .i{flex:none}
.tx .ft .r{display:flex;gap:8px}
@media (max-width:1240px){ .tx{display:none} }
@media (max-width:860px){ th:nth-child(4),td:nth-child(4),th:nth-child(6),td:nth-child(6){display:none} }
''', top('Search packages') + f'''
    <section class="fm sw">
      <div class="swm">
        <div class="sbar">{TABS().replace('class="ftab on">Updates','class="ftab">Updates').replace('<button class="ftab">Installed','<button class="ftab on">Installed')}<div class="sp"></div><div class="fsearch">{I('search')}Filter installed</div></div>
        <div class="chips"><span class="on">All 883</span><span>Installed by you 76</span><span>Dependencies 807</span><span>Orphans 12</span><span>AUR 9</span><span>Flatpak 5</span></div>
        <div class="tb"><table><thead><tr><th>Package</th><th>Version</th><th>Source</th><th>Size</th><th>Reason</th><th>Installed</th></tr></thead><tbody>{trs}</tbody></table></div>
      </div>
      <aside class="tx">
        <div class="hd"><b>Updating system</b><div class="bar"><i></i></div><div class="row"><span>4 of 18 packages</span><span>About 3 min left</span></div></div>
        <pre>{TX}</pre>
        <div class="ft">
          <div class="warn">{I('alert')}<div>linux is being upgraded. Reboot when this finishes to use kernel 7.2.7.</div></div>
          <div class="r"><button class="abtn">{I('terminal')}Open in terminal</button><button class="abtn">Hide</button></div>
        </div>
      </aside>
    </section>''')

write('016-software', {'a': A, 'b': B, 'c': C})
