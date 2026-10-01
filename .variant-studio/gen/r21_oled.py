import os, re
ROUND = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'rounds')
src = open(os.path.join(ROUND, '003-remix-rail-colori', 'a.html')).read()
src = src.replace('<div class="q">', '<div class="q oled">', 1)

HUES = '''
  --h-ov:#8B93FF; --h-term:#3DDC97; --h-file:#5AB0FF; --h-log:#FFB547;
  --h-svc:#FF6B81; --h-sw:#B98CFF; --h-usr:#3FD8DE; --h-plg:#FF7AC6; --distro:#3AB4F2;
  --ink:#F4F4F6; --ink2:#A3A3AD; --ink3:#6C6C76;
'''
def soft(p):  # tinted fills derived from each hue
    return ''.join(f'--h-{k}-s:color-mix(in srgb,var(--h-{k}) {p}%,#000);' for k in ['ov','term','file','log','svc','sw','usr','plg']) + f'--distro-s:color-mix(in srgb,var(--distro) {p}%,#000);'

NOLINES = '''
.oled .al{border-top:0;padding:10px 0}
.oled .btn{border:0;background:var(--sunk)}
.oled .btn:hover{background:var(--line)}
.oled .lsearch,.oled .item{border:0}
.oled .tools button{box-shadow:none}
'''
V = {
 'a': ('Lifted panels', '--bg:#000;--surface:#0E0E10;--sunk:#18181B;--line:#26262B;' + soft(16), ''),
 'b': ('All black, colour only', '--bg:#000;--surface:#000;--sunk:#0D0D0F;--line:#1C1C20;' + soft(13),
       '.oled .rail{background:#000}.oled .canvas{padding-left:4px;padding-right:4px}.oled .search,.oled .pill,.oled .ib{background:#111113}'),
 'c': ('Layered greys', '--bg:#000;--surface:#141416;--sunk:#1F1F23;--line:#2C2C31;' + soft(20), '.oled .card{background:#1B1B1E}'),
}
for k, (label, tok, extra) in V.items():
    css = f'<style>html .q.oled.oled{{{tok}{HUES}}}{NOLINES}{extra}</style>\n'
    open(os.path.join(ROUND, '021-oled', f'{k}.html'), 'w').write(src.replace('<div class="q oled">', css + '<div class="q oled">', 1))
print('ok')
