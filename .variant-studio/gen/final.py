# Final look for new mock-ups: OLED default theme, "rounded rectangles" components, no grey lines.
import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from base import *
import r23_components as K

OLED = '''html .q.fin.fin{--bg:#000;--surface:#0E0E10;--sunk:#18181B;--line:#26262B;--ink:#F4F4F6;--ink2:#A3A3AD;--ink3:#6C6C76;
--h-ov:#8B93FF;--h-term:#3DDC97;--h-file:#5AB0FF;--h-log:#FFB547;--h-svc:#FF6B81;--h-sw:#B98CFF;--h-usr:#3FD8DE;--h-plg:#FF7AC6;--distro:#3AB4F2;
''' + ''.join(f'--h-{k}-s:color-mix(in srgb,var(--h-{k}) 16%,#000);' for k in ['ov','term','file','log','svc','sw','usr','plg']) + '''--distro-s:color-mix(in srgb,var(--distro) 16%,#000);
--acc:var(--h-file);--acc-s:var(--h-file-s);--on-acc:#000;
--ok:#3DDC97;--ok-s:color-mix(in srgb,var(--ok) 16%,#000);--warn:#FFB547;--warn-s:color-mix(in srgb,var(--warn) 16%,#000);
--err:#FF6B81;--err-s:color-mix(in srgb,var(--err) 16%,#000);--info:#5AB0FF;--info-s:color-mix(in srgb,var(--info) 16%,#000);color-scheme:dark}
.q.fin .rail a.it .pi{background:var(--s);color:var(--h)}
.q.fin .rail a.it.on{color:var(--h)}
.q.fin .rail a.it.on .pi{background:var(--h);color:#000}
.q.fin .rsep{width:36px;height:1px;background:var(--line);margin:6px 0}
.q.fin{--mono:"JetBrains Mono",ui-monospace,monospace}
.q.fin .mono{font-family:var(--mono)}
'''
FINAL_CSS = OLED + K.BASE + K.STYLES['b'][1].replace('.k{', '.q.fin{')

def fpage(cls, css, main, extra_active='Docker', js=''):
    html = page('fin ' + cls, FINAL_CSS + css, main, 'none', js, (('c-file','server','Docker'),))
    html = html.replace('<a class="it c-file" href="#"><span class="pi"><svg class="i"><use href="#i-server"/></svg></span><span class="lb">Docker</span>',
                        '<a class="it c-file on" href="#"><span class="pi"><svg class="i"><use href="#i-server"/></svg></span><span class="lb">Docker</span>')
    return html
