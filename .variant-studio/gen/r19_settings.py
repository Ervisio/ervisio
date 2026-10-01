import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *

def spage(cls, css, main, js=''):
    return page(cls, css, main, 'c-set', js)

ST_CSS = '''
.c-set{--h:var(--ink);--s:var(--sunk)}
.sw{width:42px;height:24px;border-radius:999px;background:var(--line);position:relative;flex:none;display:inline-block;cursor:pointer}
.sw::after{content:"";position:absolute;top:3px;left:3px;width:18px;height:18px;border-radius:50%;background:var(--surface);transition:left .15s}
.sw.on{background:var(--h-ov)}.sw.on::after{left:21px}
.seg2{display:inline-flex;background:var(--sunk);border-radius:999px;padding:3px;gap:2px;flex-wrap:wrap}
.seg2 button{height:32px;padding:0 14px;border-radius:999px;font-weight:700;font-size:13px;color:var(--ink3)}
.seg2 button.on{background:var(--surface);color:var(--ink);box-shadow:0 1px 2px rgba(0,0,0,.1)}
.sel{display:inline-flex;align-items:center;gap:10px;height:38px;padding:0 12px 0 14px;border-radius:12px;background:var(--sunk);font-weight:600;min-width:180px;justify-content:space-between}
.sel .i{width:16px;height:16px;color:var(--ink3)}
.inp{display:inline-flex;align-items:center;height:38px;padding:0 14px;border-radius:12px;background:var(--sunk);font:13px var(--mono);min-width:180px}
.set{display:flex;align-items:center;gap:20px;padding:14px 0;border-top:1px solid var(--line)}
.set:first-of-type{border-top:0}
.set .tx{flex:1;min-width:0}
.set b{display:block;font-weight:700}
.set small{display:block;color:var(--ink3);font-size:13px;margin-top:2px;max-width:60ch}
.set .key{display:inline-block;font:11.5px var(--mono);color:var(--ink3);margin-top:4px}
.grp{background:var(--surface);border-radius:20px;padding:6px 20px;border:1px solid var(--line)}
.gt{display:flex;align-items:center;gap:10px;margin:22px 0 10px;font-size:17px;font-weight:800}
.gt:first-child{margin-top:0}
.gt .ft-ic{width:34px;height:34px;border-radius:11px}
.lockb{display:inline-flex;align-items:center;gap:5px;font-size:11.5px;font-weight:800;padding:2px 8px;border-radius:999px;background:var(--h-log-s);color:var(--h-log);margin-left:6px}
.lockb .i{width:12px;height:12px}
.themes{display:flex;gap:10px;flex-wrap:wrap}
.th{width:132px;border-radius:16px;padding:6px;border:2px solid var(--line);cursor:pointer}
.th.on{border-color:var(--h-ov)}
.th .pv{height:74px;border-radius:11px;display:grid;grid-template-columns:18px 1fr;gap:5px;padding:6px;box-sizing:border-box}
.th .pv i{border-radius:5px;display:block}
.th .pv .c{display:grid;grid-template-rows:12px 1fr;gap:5px}
.th b{display:flex;align-items:center;justify-content:space-between;font-size:13px;font-weight:700;padding:8px 4px 2px}
.th small{font-size:11.5px;color:var(--ink3);font-weight:600}
.th.dark .pv{background:#13151E}.th.dark .pv i{background:#2A2E40}.th.dark .pv i.a{background:#9BA3FF}
.th.light .pv{background:#ECEEF4}.th.light .pv i{background:#fff}.th.light .pv i.a{background:#4651D0}
.th.sys .pv{background:linear-gradient(135deg,#ECEEF4 50%,#13151E 50%)}.th.sys .pv i{background:rgba(127,127,127,.35)}
.warnl{display:flex;gap:10px;padding:10px 12px;border-radius:12px;background:var(--h-log-s);color:var(--h-log);font-size:13px;font-weight:600;margin:0 0 12px}
.warnl .i{flex:none}
.hostr{display:flex;align-items:center;gap:12px;padding:12px 0;border-top:1px solid var(--line)}
.hostr:first-of-type{border-top:0}
.hostr b{font-weight:700}
.hostr small{color:var(--ink3);font-size:12.5px;display:block}
.hostr .st{margin-left:auto;font-size:12px;font-weight:700}
.dot{width:9px;height:9px;border-radius:50%;flex:none}
.abtn{display:inline-flex;align-items:center;gap:8px;height:38px;padding:0 14px;border-radius:999px;font-weight:700;font-size:13.5px;background:var(--sunk)}
.abtn:hover{background:var(--line)}
.abtn.p{background:var(--h-ov);color:var(--surface)}
.abtn .i{width:16px;height:16px}
.toast{position:fixed;right:24px;bottom:24px;display:flex;align-items:center;gap:10px;padding:12px 16px;border-radius:16px;background:var(--ink);color:var(--surface);font-weight:700;box-shadow:0 12px 30px -10px rgba(0,0,0,.5);z-index:9}
.toast .i{color:var(--h-term-s)}
.toast button{margin-left:8px;color:var(--surface);opacity:.75;font-weight:700}
@media (max-width:860px){ .set{flex-wrap:wrap} }
'''

def S(title, desc, ctl, key=''):
    k = f'<span class="key">{key}</span>' if key else ''
    return f'<div class="set"><div class="tx"><b>{title}</b>{"<small>"+desc+"</small>" if desc else ""}{k}</div>{ctl}</div>'
SW = lambda on: f'<span class="sw{" on" if on else ""}" role="switch" aria-checked="{"true" if on else "false"}"></span>'
SEL = lambda v: f'<span class="sel">{v}{I("chevron")}</span>'
SEG = lambda opts, on: '<span class="seg2">' + ''.join(f'<button class="{"on" if o==on else ""}">{o}</button>' for o in opts) + '</span>'
INP = lambda v: f'<span class="inp">{v}</span>'

THEMES = f'''<div class="themes">
  <div class="th dark on"><div class="pv"><i class="a"></i><div class="c"><i></i><i></i></div></div><b>Dark <small>Default</small></b></div>
  <div class="th light"><div class="pv"><i class="a"></i><div class="c"><i></i><i></i></div></div><b>Light</b></div>
  <div class="th sys"><div class="pv"><i></i><div class="c"><i></i><i></i></div></div><b>System</b></div></div>'''

# ---- section bodies (shared by all variants) ----
SEC = {}
SEC['appearance'] = ('Appearance','palette','t-img', False, f'''
  <div class="set" style="display:block"><div class="tx" style="margin-bottom:12px"><b>Theme</b><small>Saved to your account, so it follows you to any browser.</small></div>{THEMES}</div>
  {S('Density','Compact fits more rows in tables and lists.',SEG(['Comfortable','Compact'],'Comfortable'))}
  {S('Reduce motion','Turns off widget wiggle, pulses and transitions.',SW(False))}''')
SEC['language'] = ('Language and region','globe','t-doc', False, f'''
  {S('Language','',SEL('English'))}
  {S('Time format','',SEG(['24-hour','12-hour'],'24-hour'))}
  {S('First day of the week','',SEL('Monday'))}''')
SEC['terminal'] = ('Terminal','terminal','t-code', False, f'''
  {S('Colour theme','Follow app uses the dark or light palette of Quadro.',SEL('Follow app'))}
  {S('Font size','',SEG(['12','13','14','16'],'14'))}
  {S('Copy on select','',SW(True))}
  {S('Show snippet chips','The row of saved commands under the terminal.',SW(True))}''')
SEC['notify'] = ('Notifications','bell','t-arc', False, f'''
  {S('A service fails','',SW(True))}
  {S('Updates are ready','',SW(True))}
  {S('Failed SSH logins','More than 10 in 5 minutes from the same address.',SW(True))}
  {S('Browser notifications','Show them even when Quadro is in a background tab.',SW(False))}''')
SEC['signin'] = ('Sign-in and security','key','t-pdf', True, f'''
  <div class="warnl">{I('alert')}<div>Changes here apply to every user of this server.</div></div>
  {S('Allow root to sign in','Off is safer. Admins can still use sudo.',SW(False),'allow_root = false')}
  {S('Show IP address on the sign-in page','Anyone who opens the page sees it.',SW(True),'login.show_ip = true')}
  {S('Sign out after','',SEL('12 hours'),'session.timeout = 12h')}
  {S('Two-factor sign-in','Ask admins for a code from an authenticator app.',SEG(['Off','Admins','Everyone'],'Admins'),'totp = admins')}
  {S('Block after failed attempts','Per address, for 15 minutes.',SEL('5 attempts'),'login.max_failures = 5')}
  {S('Allowed networks','Leave empty to allow any address.',INP('192.168.1.0/24'),'listen.allow = ["192.168.1.0/24"]')}''')
SEC['web'] = ('Web server','net','t-dir', True, f'''
  {S('Address and port','',INP('0.0.0.0:9090'),'listen = "0.0.0.0:9090"')}
  {S('Certificate',"Let's Encrypt needs a public domain name.",SEG(['Self-signed',"Let's Encrypt",'Custom'],'Self-signed'),'tls.mode = "self-signed"')}
  {S('Redirect HTTP to HTTPS','',SW(True),'tls.redirect = true')}''')
SEC['plugpol'] = ('Plugins','plugins','t-plg', True, f'''
  {S('Allow community plugins','Plugins not signed by the Quadro team.',SW(True),'plugins.allow_unsigned = true')}
  {S('Developer mode','Load plugins from a folder and reload them on save.',SW(False),'plugins.dev = false')}''')
SEC['hosts'] = ('Hosts','server','t-txt', True, f'''
  <div class="hostr"><span class="dot" style="background:var(--h-term)"></span><div><b>arch</b><small>This machine, 192.168.1.137</small></div><span class="st" style="color:var(--h-term)">Online</span></div>
  <div class="hostr"><span class="dot" style="background:var(--h-term)"></span><div><b>backup-nas</b><small>SSH key, 192.168.1.40</small></div><span class="st" style="color:var(--h-term)">Online</span></div>
  <div class="hostr"><span class="dot" style="background:var(--ink3)"></span><div><b>web-01</b><small>SSH key, web-01.example.org</small></div><span class="st muted">Last seen 3 days ago</span></div>
  <div style="padding:12px 0"><button class="abtn">{I('plus')}Add host</button></div>''')
SEC['about'] = ('About','info','t-txt', False, f'''
  {S('Quadro 0.1.0','Up to date. Checked today at 16:40.',f'<button class="abtn">{I("refresh")}Check</button>')}
  {S('Configuration file','',INP('/etc/quadro/quadro.conf'))}''')

YOU = ['appearance','language','terminal','notify']
SRV = ['signin','web','plugpol','hosts']
ALL = YOU + SRV + ['about']

def ft(c): return f'<span class="ft-ic {c}"' + (' style="--h:var(--h-plg);--s:var(--h-plg-s)"' if c=='t-plg' else '') + '>'

def section(k, with_title=True):
    title, ic, c, admin, body = SEC[k]
    lb = f'<span class="lockb">{I("lock")}Admins</span>' if admin else ''
    t = f'<div class="gt" id="s-{k}">{ft(c)}{I(ic)}</span>{title}{lb}</div>' if with_title else ''
    return f'{t}<div class="grp">{body}</div>'

TOAST = f'<div class="toast">{I("check")}Saved<button>Undo</button></div>'

# ===== A: section list + one section at a time =====
def navitem(k, on):
    title, ic, c, admin, _ = SEC[k]
    return f'<a class="ni{" on" if on else ""}" href="#" data-s="{k}">{ft(c)}{I(ic)}</span>{title}{" "+I("lock") if admin else ""}</a>'
panes = ''.join(f'<div class="pn{" on" if k=="appearance" else ""}" data-p="{k}">{section(k)}</div>' for k in ALL)
JS_A = '''<script>
document.querySelector('.snav').addEventListener('click',e=>{const a=e.target.closest('[data-s]');if(!a)return;e.preventDefault();
document.querySelectorAll('.ni').forEach(x=>x.classList.toggle('on',x===a));document.querySelectorAll('.pn').forEach(p=>p.classList.toggle('on',p.dataset.p===a.dataset.s))});
document.querySelectorAll('.sw').forEach(s=>s.addEventListener('click',()=>s.classList.toggle('on')));
</script>'''
A = spage('va', ST_CSS + '''
.st{flex:1;display:flex;min-height:calc(100vh - 90px)}
.snav{width:260px;flex:none;border-right:1px solid var(--line);padding:16px 12px;display:flex;flex-direction:column;gap:2px;overflow:auto}
.snav h2{margin:0 10px 12px;font-size:20px;font-weight:800}
.snav .gl{font-size:12.5px;color:var(--ink3);font-weight:700;margin:14px 10px 4px}
.ni{display:flex;align-items:center;gap:10px;padding:6px 10px 6px 6px;border-radius:14px;color:var(--ink);text-decoration:none;font-weight:600}
.ni:hover{background:var(--sunk)}
.ni.on{background:var(--h-ov-s)}
.ni .ft-ic{width:30px;height:30px;border-radius:10px}.ni .ft-ic .i{width:15px;height:15px}
.ni > .i{width:13px;height:13px;margin-left:auto;color:var(--ink3)}
.sbody{flex:1;min-width:0;padding:22px 28px;overflow:auto;max-width:860px}
.pn{display:none}.pn.on{display:block}
@media (max-width:1000px){ .snav{display:none} .pn{display:block} }
''', top('Search settings') + f'''
    <section class="fm st">
      <nav class="snav"><h2>Settings</h2>
        <div class="gl">You</div>{''.join(navitem(k, k=='appearance') for k in YOU)}
        <div class="gl">This server</div>{''.join(navitem(k, False) for k in SRV)}
        <div class="gl"></div>{navitem('about', False)}
      </nav>
      <div class="sbody">{panes}</div>
    </section>{TOAST}''', JS_A)

# ===== B: one long page with search and table of contents =====
toc = ''.join(f'<a href="#s-{k}" class="{"on" if k=="appearance" else ""}">{SEC[k][0]}</a>' for k in ALL)
JS_B = '''<script>document.querySelectorAll('.sw').forEach(s=>s.addEventListener('click',()=>s.classList.toggle('on')));</script>'''
B = spage('vb', ST_CSS + '''
.st{flex:1;display:flex;min-height:calc(100vh - 90px)}
.scroll{flex:1;min-width:0;padding:22px 28px 60px;overflow:auto}
.scroll .inner{max-width:820px;margin:0 auto}
.hs{display:flex;align-items:center;gap:12px;height:52px;padding:0 20px;border-radius:999px;background:var(--sunk);color:var(--ink3);font-size:15px;margin-bottom:26px}
.hs .i{width:20px;height:20px}
.part{font-size:13px;font-weight:800;color:var(--ink3);margin:34px 0 4px;display:flex;align-items:center;gap:10px}
.part::after{content:"";flex:1;height:1px;background:var(--line)}
.toc{width:220px;flex:none;padding:22px 16px;border-left:1px solid var(--line);position:sticky;top:0;align-self:flex-start;display:flex;flex-direction:column;gap:2px}
.toc b{font-weight:800;margin:0 10px 8px}
.toc a{padding:7px 10px;border-radius:10px;color:var(--ink2);text-decoration:none;font-weight:600;font-size:13.5px;border-left:3px solid transparent}
.toc a.on{color:var(--h-ov);background:var(--h-ov-s)}
@media (max-width:1240px){ .toc{display:none} }
''', top('Search settings') + f'''
    <section class="fm st">
      <div class="scroll"><div class="inner">
        <div class="hs">{I('search')}Search settings, e.g. “root” or “port”</div>
        <div class="part">You</div>{''.join(section(k) for k in YOU)}
        <div class="part">This server, admins only</div>{''.join(section(k) for k in SRV)}
        <div class="part">Quadro</div>{section('about')}
      </div></div>
      <nav class="toc"><b>On this page</b>{toc}</nav>
    </section>{TOAST}''', JS_B)

# ===== C: Personal / Server tabs, server tab shows pending config diff =====
JS_C = '''<script>
document.querySelector('.bigtabs').addEventListener('click',e=>{const b=e.target.closest('[data-t]');if(!b)return;
document.querySelectorAll('.bigtabs button').forEach(x=>x.classList.toggle('on',x===b));document.querySelectorAll('.tp').forEach(p=>p.classList.toggle('on',p.dataset.t===b.dataset.t))});
document.querySelectorAll('.sw').forEach(s=>s.addEventListener('click',()=>{s.classList.toggle('on');document.querySelector('.pend').hidden=false}));
</script>'''
DIFF = '''<span class="d">  [login]</span>
<span class="m">- show_ip = true</span>
<span class="p">+ show_ip = false</span>
<span class="d">  [session]</span>
<span class="m">- timeout = "12h"</span>
<span class="p">+ timeout = "4h"</span>'''
C = spage('vc', ST_CSS + '''
.st{flex:1;display:flex;flex-direction:column;min-height:calc(100vh - 90px)}
.bigtabs{display:flex;gap:8px;padding:16px 22px;border-bottom:1px solid var(--line)}
.bigtabs button{display:flex;align-items:center;gap:12px;padding:10px 18px 10px 10px;border-radius:18px;background:var(--sunk);text-align:left;border:2px solid transparent}
.bigtabs button.on{border-color:var(--h-ov);background:var(--h-ov-s)}
.bigtabs .ft-ic{width:40px;height:40px;border-radius:13px}
.bigtabs b{display:block;font-weight:800;font-size:15px}
.bigtabs small{color:var(--ink3);font-size:12.5px}
.tp{display:none;flex:1;min-height:0}
.tp.on{display:flex}
.cols{flex:1;min-width:0;padding:22px;overflow:auto;display:grid;grid-template-columns:repeat(auto-fit,minmax(380px,1fr));gap:0 22px;align-content:start}
.cols > div{min-width:0}
.cfg{width:360px;flex:none;border-left:1px solid var(--line);padding:18px;display:flex;flex-direction:column;gap:12px}
.cfg h3{margin:0;font-size:16px;font-weight:800}
.cfg small{color:var(--ink3)}
.cfg pre{margin:0;padding:12px;border-radius:14px;background:var(--sunk);font:12.5px/1.7 var(--mono);color:var(--ink2);white-space:pre-wrap}
.cfg .m{color:var(--h-svc);background:var(--h-svc-s);display:inline-block;border-radius:4px;padding:0 4px}
.cfg .p{color:var(--h-term);background:var(--h-term-s);display:inline-block;border-radius:4px;padding:0 4px}
.cfg .d{color:var(--ink3)}
.cfg .r{display:flex;gap:8px}
.pend[hidden]{display:none}
@media (max-width:1240px){ .cfg{display:none} }
''', top('Search settings') + f'''
    <section class="fm st">
      <div class="bigtabs">
        <button class="on" data-t="you">{ft('t-usr')}{I('users')}</span><div><b>Personal</b><small>Only for fonlogen</small></div></button>
        <button data-t="srv">{ft('t-pdf')}{I('server')}</span><div><b>Server</b><small>Admins, applies to everyone</small></div></button>
      </div>
      <div class="tp on" data-t="you"><div class="cols">{''.join(f"<div>{section(k)}</div>" for k in YOU)}</div></div>
      <div class="tp" data-t="srv"><div class="cols">{''.join(f"<div>{section(k)}</div>" for k in SRV + ['about'])}</div>
        <aside class="cfg"><h3>Pending changes</h3><small>Written to <span class="mono">/etc/quadro/quadro.conf</span> when you apply. A backup of the old file is kept.</small>
          <pre class="pend">{DIFF}</pre>
          <div class="r"><button class="abtn">Discard</button><button class="abtn p">{I('check')}Apply 2 changes</button></div></aside></div>
    </section>''', JS_C)

if __name__ == '__main__':
    write('019-settings', {'a': A, 'b': B, 'c': C})
