# Builds docs/design/: standalone copies of every approved mock + the summary page.
import os, re, shutil
HERE = os.path.dirname(os.path.abspath(__file__))
ROUNDS = os.path.join(HERE, '..', 'rounds')
OUT = os.path.abspath(os.path.join(HERE, '..', '..', 'docs', 'design'))
BASE_CSS = open(os.path.expanduser('~/.claude/skills/variant-studio/scripts/ui/base.css')).read()

OLED = '''html body .q.q{--bg:#000;--surface:#0E0E10;--sunk:#18181B;--line:#26262B;--ink:#F4F4F6;--ink2:#A3A3AD;--ink3:#6C6C76;
--h-ov:#8B93FF;--h-term:#3DDC97;--h-file:#5AB0FF;--h-log:#FFB547;--h-svc:#FF6B81;--h-sw:#B98CFF;--h-usr:#3FD8DE;--h-plg:#FF7AC6;--distro:#3AB4F2;
''' + ''.join(f'--h-{k}-s:color-mix(in srgb,var(--h-{k}) 16%,#000);' for k in ['ov','term','file','log','svc','sw','usr','plg']) + '--distro-s:color-mix(in srgb,var(--distro) 16%,#000);}'

# slug, round/variant, width
SCREENS = [
 ('overview','021-oled/a',1280), ('sign-in','007-login-netflix/a',1280), ('files','009-files-remix/c',1280),
 ('terminal','011-terminal-remix/a',1280), ('logs','012-logs/a',1280), ('services','014-services-v2/b',1280),
 ('users','015-users/b',1280), ('software','016-software/a',1280), ('plugins','018-plugins-remix/a',1280),
 ('settings','020-settings-remix/c',1280), ('themes','022-themes/c',1280), ('components','023-components/b',1280),
 ('mobile','024-mobile/a',1600),
]

def main():
    os.makedirs(os.path.join(OUT, 'screens'), exist_ok=True)
    shutil.copy(os.path.join(ROUNDS, '024-mobile', 'kit.js'), os.path.join(OUT, 'screens', 'kit.js'))
    shutil.copy(os.path.join(ROUNDS, '007-login-netflix', 'login.js'), os.path.join(OUT, 'screens', 'login.js'))
    for slug, rv, w in SCREENS:
        rnd, v = rv.split('/')
        frag = open(os.path.join(ROUNDS, rnd, v + '.html')).read()
        shared = open(os.path.join(ROUNDS, rnd, '_shared.css')).read() if os.path.exists(os.path.join(ROUNDS, rnd, '_shared.css')) else ''
        doc = f'''<!doctype html>
<html lang="en" data-theme="dark" class="dark" style="color-scheme:dark">
<head><meta charset="utf-8"><meta name="viewport" content="width={w}"><title>Quadro, {slug}</title>
<style>{BASE_CSS}</style><style>{shared}</style><style>{OLED}</style></head>
<body>{frag}</body></html>'''
        open(os.path.join(OUT, 'screens', slug + '.html'), 'w').write(doc)
    print('screens ok ->', OUT)

if __name__ == '__main__':
    main()
