"""Round 033: Ervisio logo proposals. Each variant is a brand sheet: mark + wordmark, app icon, favicons, light use, README header."""
import json, os

ROUND = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'rounds', '033-ervisio-logo')
HUES = ['#8B93FF', '#3DDC97', '#5AB0FF', '#FFB547', '#FF6B81', '#B98CFF', '#3FD8DE', '#FF7AC6']  # ov term file log svc sw usr plg

# ---------------------------------------------------------------- marks (viewBox 0 0 100 100)

def mark_visor(fg='#8B93FF', cur='#3DDC97'):
    # a window/eye: rounded frame with a horizontal slit; the pupil is a terminal block cursor
    return (f'<svg viewBox="0 0 100 100"><rect x="8" y="22" width="84" height="56" rx="28" fill="none" stroke="{fg}" stroke-width="9"/>'
            f'<rect x="53" y="38" width="13" height="24" rx="3" fill="{cur}"/><rect x="31" y="47" width="16" height="6" rx="3" fill="{fg}" opacity=".55"/></svg>')

def mark_arch(fg='#F4F4F6', key='#8B93FF'):
    # Roman arch; the keystone is a V
    return (f'<svg viewBox="0 0 100 100"><path d="M14 90V52a36 36 0 0 1 72 0v38H70V52a20 20 0 0 0-40 0v38z" fill="{fg}"/>'
            f'<path d="M38 8h24l-12 26z" fill="{key}"/></svg>')

def mark_spectrum(ink='#000', iris='#F4F4F6'):
    # eight arcs in the section colours around an eye
    import math
    arcs = ''
    for i, c in enumerate(HUES):
        a0 = math.radians(-90 + i * 45 + 4); a1 = math.radians(-90 + (i + 1) * 45 - 4)
        x0, y0 = 50 + 42 * math.cos(a0), 50 + 42 * math.sin(a0)
        x1, y1 = 50 + 42 * math.cos(a1), 50 + 42 * math.sin(a1)
        arcs += f'<path d="M{x0:.2f} {y0:.2f}A42 42 0 0 1 {x1:.2f} {y1:.2f}" fill="none" stroke="{c}" stroke-width="10" stroke-linecap="round"/>'
    eye = (f'<path d="M22 50Q50 24 78 50Q50 76 22 50z" fill="{iris}"/><circle cx="50" cy="50" r="10" fill="{ink}"/>'
           f'<circle cx="54" cy="46" r="3" fill="{iris}"/>')
    return f'<svg viewBox="0 0 100 100">{arcs}{eye}</svg>'

def mark_owl(fg='#F4F4F6', acc='#8B93FF', ink='#000'):
    # noctua: two eyes, the brows meet in a V
    return (f'<svg viewBox="0 0 100 100"><circle cx="31" cy="56" r="21" fill="{fg}"/><circle cx="69" cy="56" r="21" fill="{fg}"/>'
            f'<circle cx="34" cy="58" r="9" fill="{ink}"/><circle cx="66" cy="58" r="9" fill="{ink}"/>'
            f'<path d="M8 20L50 44L92 20" fill="none" stroke="{acc}" stroke-width="10" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="M44 74L50 84L56 74z" fill="{acc}"/></svg>')

# ---------------------------------------------------------------- sheet

CSS = '''
.bs{--bg:#000;--surface:#0E0E10;--sunk:#18181B;--ink:#F4F4F6;--ink2:#A3A3AD;--acc:#8B93FF;
  background:var(--bg);color:var(--ink);font-family:Figtree,system-ui,sans-serif;min-height:100vh;padding:40px;box-sizing:border-box;
  display:grid;grid-template-columns:1.25fr 1fr;grid-template-rows:auto auto;gap:16px}
.bs *{box-sizing:border-box}
.bs .hero{grid-row:span 2;background:var(--surface);border-radius:28px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:28px;padding:48px;min-height:520px}
.bs .lock{display:flex;align-items:center;gap:22px}
.bs .lock svg{width:96px;height:96px}
.bs .wm{font-size:72px;font-weight:800;letter-spacing:-.045em;line-height:1}
.bs .tag{color:var(--ink2);font-size:17px;max-width:34ch;text-align:center;line-height:1.5}
.bs .card{background:var(--surface);border-radius:22px;padding:24px;display:flex;flex-direction:column;gap:18px}
.bs .card h4{margin:0;font-size:13px;font-weight:700;color:var(--ink2)}
.bs .row{display:flex;align-items:flex-end;gap:22px;flex-wrap:wrap}
.bs .tile{width:96px;height:96px;border-radius:24px;display:grid;place-items:center;background:var(--sunk)}
.bs .tile svg{width:62px;height:62px}
.bs .tile.acc{background:var(--acc)}
.bs .fav{display:flex;flex-direction:column;align-items:center;gap:8px;font-size:12px;color:var(--ink2)}
.bs .fav i{display:grid;place-items:center;background:var(--sunk);border-radius:8px}
.bs .tab{display:flex;align-items:center;gap:8px;height:34px;padding:0 14px;border-radius:10px 10px 0 0;background:var(--sunk);font-size:13px;color:var(--ink)}
.bs .tab svg{width:16px;height:16px}
.bs .light{background:#F6F6F8;color:#141418;border-radius:18px;padding:22px;display:flex;align-items:center;gap:14px}
.bs .light svg{width:44px;height:44px}
.bs .light b{font-size:30px;font-weight:800;letter-spacing:-.04em}
.bs .readme{border-radius:18px;background:#0d1117;padding:22px;display:flex;align-items:center;gap:14px}
.bs .readme svg{width:40px;height:40px}
.bs .readme b{font-size:28px;font-weight:800;letter-spacing:-.04em;color:#e6edf3}
.bs .readme span{color:#8d96a0;font-size:14px;margin-left:auto}
@media (max-width:900px){.bs{grid-template-columns:1fr;padding:16px}.bs .hero{grid-row:auto;min-height:380px}.bs .wm{font-size:48px}.bs .lock svg{width:64px;height:64px}}
'''

TAG = 'Run your Linux server from the browser.'

def sheet(mark, wm_style='', wm_text='ervisio', light_mark=None, tile_acc=False, extra_css=''):
    small = mark
    lm = light_mark or mark
    fav = ''.join(f'<span class="fav"><i style="width:{s+12}px;height:{s+12}px">{small.replace("<svg ", f"<svg width=\"{s}\" height=\"{s}\" ")}</i>{s} px</span>' for s in (32, 16))
    return f'''<style>{CSS}{extra_css}</style><div class="bs">
<section class="hero"><div class="lock">{mark}<span class="wm" style="{wm_style}">{wm_text}</span></div><p class="tag">{TAG}</p></section>
<section class="card"><h4>App icon and favicons</h4><div class="row"><span class="tile{' acc' if tile_acc else ''}">{mark}</span>{fav}
<span class="tab">{small}Ervisio</span></div></section>
<section class="card"><h4>On light and in a README</h4><div class="light">{lm}<b style="{wm_style}">{wm_text}</b></div>
<div class="readme">{mark}<b style="{wm_style}">{wm_text}</b><span>formerly LinuxAdmin</span></div></section>
</div>'''

A = sheet(mark_visor(), light_mark=mark_visor('#4B53D6', '#13A86B'))
B = sheet(mark_arch(), 'font-family:Marcellus,serif;font-weight:400;letter-spacing:.12em;text-transform:uppercase', 'Ervisio',
          light_mark=mark_arch('#141418', '#4B53D6'), extra_css='.bs .hero .wm{font-size:56px}.bs .light b,.bs .readme b{font-size:24px}')
C = sheet(mark_spectrum(), light_mark=mark_spectrum('#F6F6F8', '#141418'))
D = sheet(mark_owl(), light_mark=mark_owl('#141418', '#4B53D6', '#F6F6F8'))

LABELS = {
    'a': ('Visor', 'A window that is also an eye; the pupil is a terminal cursor. Reads as "a console that watches".'),
    'b': ('Keystone', 'A Roman arch whose keystone is the V: the Latin root, carved. Inscription-style wordmark.'),
    'c': ('Spectrum', 'An eye ringed by the eight section colours: one place that sees every part of the server.'),
    'd': ('Noctua', "Athena's owl keeping watch at night; the brows meet in a V. Works as a mascot too."),
}

if __name__ == '__main__':
    for v, html in zip('abcd', (A, B, C, D)):
        open(os.path.join(ROUND, v + '.html'), 'w').write(html)
    rj = os.path.join(ROUND, 'round.json'); r = json.load(open(rj))
    r['head'] = ['<link rel="preconnect" href="https://fonts.googleapis.com">',
                 '<link href="https://fonts.googleapis.com/css2?family=Figtree:wght@400;700;800&family=Marcellus&display=swap" rel="stylesheet">']
    r['viewports'] = ['laptop', 'mobile']
    for x in r['variants']:
        x['label'], x['notes'] = LABELS[x['id']]
    json.dump(r, open(rj, 'w'), indent=2)
