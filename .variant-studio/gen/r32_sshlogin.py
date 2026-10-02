"""Round 032: SSH key sign-in, built on the real sign-in page (web/dist CSS, DOM captured from the app)."""
import json, os

ROUND = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'rounds', '032-login-sshkey')
TOKENS = ('--bg:#000;--surface:#0E0E10;--sunk:#18181B;--line:#26262B;--ink:#F4F4F6;--ink2:#A3A3AD;--ink3:#6C6C76;'
          '--distro:#1793D1;--distro-s:color-mix(in srgb,#1793D1 16%,#000);--acc:#8B93FF;--acc-s:color-mix(in srgb,#8B93FF 18%,#000);'
          '--on-acc:#000;--ok:#3DDC97;--ok-s:color-mix(in srgb,#3DDC97 16%,#000);--warn:#FFB547;--warn-s:color-mix(in srgb,#FFB547 16%,#000);'
          '--err:#FF6B81;--err-s:color-mix(in srgb,#FF6B81 16%,#000);--info:#5AB0FF;--info-s:color-mix(in srgb,#5AB0FF 16%,#000);'
          '--h-term:#3DDC97;--h-term-s:color-mix(in srgb,#3DDC97 16%,#000);--h-usr:#3FD8DE;--h-usr-s:color-mix(in srgb,#3FD8DE 16%,#000);'
          '--scrim:rgba(0,0,0,.62);color-scheme:dark;')

def svg(d, extra=''):
    return f'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" class="ui-icon" {extra}>{d}</svg>'

USER = svg('<circle cx="12" cy="8" r="4"></circle><path d="M4 21a8 8 0 0 1 16 0"></path>')
SHIELD = svg('<path d="M12 3l8 3v6c0 4.5-3.4 8-8 9-4.6-1-8-4.5-8-9V6z"></path>')
EYE = svg('<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"></path><circle cx="12" cy="12" r="3"></circle>')
KEY = svg('<circle cx="7.5" cy="15.5" r="4.5"></circle><path d="M10.7 12.3L20 3"></path><path d="M16 7l3 3"></path><path d="M14 9l2 2"></path>')
CHECK = svg('<path d="M5 12.5l4.5 4.5L19 7.5"></path>')
X = svg('<path d="M6 6l12 12M18 6L6 18"></path>')
UP = svg('<path d="M12 16V4"></path><path d="M7 9l5-5 5 5"></path><path d="M4 20h16"></path>')
LOCK = svg('<rect x="5" y="11" width="14" height="10" rx="2.5"></rect><path d="M8 11V7a4 4 0 0 1 8 0v4"></path>')
MOON = svg('<path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"></path>', 'style="width:15px;height:15px"')
ARCH = ('<svg viewBox="0 0 24 24" class="big"><path fill="currentColor" d="M11.39.605C10.376 3.092 9.764 4.72 8.635 7.132c.693.734 1.543 1.589 2.923 2.554-1.484-.61-2.496-1.224-3.252-1.86C6.86 10.842 4.596 15.138 0 23.395c3.612-2.085 6.412-3.37 9.021-3.862a6.61 6.61 0 0 1-.171-1.547l.003-.115c.058-2.315 1.261-4.095 2.687-3.973 1.426.12 2.534 2.096 2.478 4.409a6.52 6.52 0 0 1-.146 1.243c2.58.505 5.352 1.787 8.914 3.844-.702-1.293-1.33-2.459-1.929-3.57-.943-.73-1.926-1.682-3.933-2.713 1.38.359 2.367.772 3.137 1.234-6.09-11.334-6.582-12.84-8.67-17.74z"></path></svg>')

ART = f'<section class="lb-art"><span class="glow"></span>{ARCH}<div class="nm"><h2>arch</h2><p class="ip">192.168.1.137</p><p class="os">Arch Linux</p></div></section>'
FOOT = f'<div class="lb-foot"><span></span><button type="button">{MOON}Theme</button></div>'

def field(label, icon, inner, extra=''):
    return f'<div class="ui-field"><label>{label}</label><div class="ui-in">{icon}{inner}{extra}</div></div>'

USERNAME = field('Username', USER, '<input value="fonlogen" autocomplete="username">')
STAY = f'<label class="ui-choice"><span class="ui-check"><input type="checkbox" checked><i>{CHECK}</i></span>Stay signed in</label>'
PASSPHRASE = field('Key passphrase', LOCK, '<input type="password" value="correcthorse" placeholder="The passphrase of this key">',
                   f'<span style="display:inline-flex"><button type="button" class="ui-btn ui-btn--ghost ui-btn--icon">{EYE}</button></span>')
SUBMIT = '<button type="submit" class="ui-btn ui-btn--primary ui-btn--lg ui-btn--block">Sign in</button>'

def keycard(cls=''):
    return (f'<div class="sk-card {cls}"><span class="sk-ic">{KEY}</span><div class="sk-meta"><b>id_ed25519</b>'
            f'<span class="mono">ED25519 · SHA256:q3Vf…9kXw</span><span class="sk-cm">fonlogen@laptop · protected by a passphrase</span></div>'
            f'<button type="button" class="ui-btn ui-btn--ghost ui-btn--icon" title="Use another key">{X}</button></div>')

NOTE = f'<p class="sk-note">{SHIELD}<span>The key stays in this browser. LinuxAdmin sends a one-time signature, never the key.</span></p>'

CSS = '''
.vs32{%s}
.vs32 .login{min-height:100vh}
.vs32 .mono{font-family:"JetBrains Mono",ui-monospace,monospace;font-size:12px}
.sk-seg{display:flex;background:var(--sunk);border-radius:12px;padding:4px;gap:4px;margin:18px 0 6px}
.sk-seg button{flex:1;height:36px;border-radius:9px;font:inherit;font-weight:700;font-size:13.5px;color:var(--ink3);background:none;border:0;display:flex;align-items:center;justify-content:center;gap:8px}
.sk-seg button .ui-icon{width:16px;height:16px}
.sk-seg button.on{background:var(--surface);color:var(--ink);box-shadow:0 1px 0 rgba(255,255,255,.03)}
.sk-card{display:flex;align-items:center;gap:12px;padding:12px 12px 12px 14px;border-radius:14px;background:var(--sunk)}
.sk-card .sk-ic{width:40px;height:40px;border-radius:12px;display:grid;place-items:center;background:var(--acc-s);color:var(--acc);flex:none}
.sk-card .sk-ic .ui-icon{width:20px;height:20px}
.sk-meta{display:flex;flex-direction:column;gap:2px;min-width:0;flex:1}
.sk-meta b{font-size:14px}.sk-meta .mono{color:var(--ink2)}.sk-cm{font-size:12px;color:var(--ink3)}
.sk-lab{font-size:13px;font-weight:700;margin:0 0 6px}
.sk-drop{display:flex;align-items:center;gap:12px;padding:16px;border-radius:14px;background:var(--sunk);box-shadow:inset 0 0 0 2px color-mix(in srgb,var(--ink3) 35%%,transparent);color:var(--ink2);font-size:13.5px}
.sk-drop .ui-icon{width:20px;height:20px;color:var(--acc)}
.sk-drop b{color:var(--ink)}
.sk-note{display:flex;gap:8px;align-items:flex-start;font-size:12.5px;color:var(--ink2);margin:2px 0 0;line-height:1.45}
.sk-note .ui-icon{width:15px;height:15px;flex:none;margin-top:1px;color:var(--ok)}
.sk-alt{display:flex;align-items:center;justify-content:center;gap:8px;width:100%%;height:44px;border-radius:12px;background:var(--sunk);color:var(--ink);font:inherit;font-weight:700;font-size:14px;border:0}
.sk-alt .ui-icon{width:17px;height:17px;color:var(--acc)}
.sk-or{display:flex;align-items:center;gap:10px;color:var(--ink2);font-size:12.5px;margin:2px 0}
.sk-or::before,.sk-or::after{content:"";flex:1}
.sk-back{background:none;border:0;color:var(--ink2);font:inherit;font-size:13px;font-weight:600;padding:0;display:inline-flex;gap:6px;align-items:center}
.sk-tabs{display:flex;gap:6px;margin:16px 0 4px}
.sk-tabs button{height:30px;padding:0 12px;border-radius:9px;border:0;background:none;color:var(--ink2);font:inherit;font-weight:700;font-size:13px}
.sk-tabs button.on{background:var(--acc-s);color:var(--acc)}
.sk-paste{width:100%%;min-height:86px;border-radius:12px;background:var(--sunk);border:0;color:var(--ink2);padding:10px 12px;font:12px "JetBrains Mono",monospace;resize:none}
.sk-pwkey{display:flex;gap:8px;align-items:center}
.sk-chip{display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 10px;border-radius:8px;background:var(--acc-s);color:var(--acc);font-size:12.5px;font-weight:700}
.sk-chip .ui-icon{width:14px;height:14px}
''' % TOKENS

def page(form, cls):
    return (f'<style>{CSS}</style><div class="vs32 {cls}"><div class="login">{ART}<section class="lb-form"><div class="lb-in">'
            f'<h1>Who\'s signing in?</h1><p>Sign in with your Linux account.</p>{form}{FOOT}</div></section></div></div>')

# A: segmented Password / SSH key at the top; key mode shows the loaded key card + passphrase.
A = page(f'''<div class="sk-seg"><button>{SHIELD}Password</button><button class="on">{KEY}SSH key</button></div>
<form class="lf">{USERNAME}<div><p class="sk-lab">Private key</p>{keycard()}</div>{PASSPHRASE}{STAY}{SUBMIT}{NOTE}</form>''', 'va')

# B: password stays the default; a secondary "Sign in with an SSH key" button below. Shown: the key step, file or paste.
B = page(f'''<form class="lf">{USERNAME}
<div><div class="sk-tabs"><button class="on">Choose a file</button><button>Paste the key</button></div>
<div class="sk-drop">{UP}<span><b>Drop your private key here</b> or choose a file. Usually ~/.ssh/id_ed25519</span></div></div>
{STAY}<button type="submit" class="ui-btn ui-btn--primary ui-btn--lg ui-btn--block" disabled style="opacity:.45">Sign in</button>
<div class="sk-or">or</div><button type="button" class="sk-alt">{SHIELD}Use my password instead</button>{NOTE}</form>''', 'vb')

# C: one form, nothing new until needed. The password field has a "Use a key" button; picking a key turns the
# field into the key card + passphrase (as in A, without the switch at the top).
C = page(f'''<form class="lf">{USERNAME}
{field('Password', SHIELD, '<input type="password" placeholder="Your Linux account password">', f'<button type="button" class="sk-chip">{KEY}Use a key</button>')}
{STAY}{SUBMIT}
<p class="sk-note">{KEY}<span>With "Use a key" you pick your SSH private key instead of typing the password. The key stays in this browser.</span></p></form>''', 'vc')

if __name__ == '__main__':
    for v, html in (('a', A), ('b', B), ('c', C)):
        with open(os.path.join(ROUND, v + '.html'), 'w') as f:
            f.write(html)
