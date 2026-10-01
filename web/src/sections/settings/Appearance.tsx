import { useEffect, useMemo, useRef, useState } from 'react';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { themeVars, useTheme, BUILTIN_THEMES, DISTRO_PRESETS, HUE_KEYS, HUE_LABEL_KEYS, parseTheme, type ColourMode, type HueKey, type ThemeDef } from '../../theme';
import { Button, Icon, Input, Segmented, toast } from '../../ui';

function Mini({ t, mode, distro }: { t: ThemeDef; mode: ColourMode; distro: string }) {
  const v = useMemo(() => themeVars(t, mode, distro), [t, mode, distro]);
  const hues = HUE_KEYS.slice(0, 5).map((k) => v[`--h-${k}`]);
  return (
    <div className="st-mini" style={{ background: t.bg }} aria-hidden="true">
      <div className="mr" style={{ background: t.surface }}>{hues.map((h, i) => <i key={i} style={{ background: h }} />)}</div>
      <div className="mc" style={{ background: t.surface }}>
        <b style={{ background: t.ink }} />
        <div className="ms">
          {(['ov', 'log', 'term'] as const).map((k) => (
            <i key={k} style={{ background: v[`--h-${k}-s`] }}><u style={{ background: v[`--h-${k}`] }} /></i>
          ))}
        </div>
        <span style={{ background: t.sunk }} />
      </div>
    </div>
  );
}

function ThemeCard({ t: def, on, onPick, onEdit, onDelete }: { t: ThemeDef; on: boolean; onPick(): void; onEdit?(): void; onDelete?(): void }) {
  const t = useT('settings');
  const th = useTheme();
  return (
    <div className="st-tc-wrap">
      <button type="button" className="st-tc" aria-pressed={on} onClick={onPick}>
        <Mini t={def} mode={th.colourMode} distro={th.distroColour} />
        <div className="nm">
          {def.name}
          {def.note && <small>{t(`note.${def.note.toLowerCase()}`)}</small>}
          {def.custom && <small>{t('appearance.custom')}</small>}
          {def.own && <span className="st-own">{t('appearance.ownColours')}</span>}
          <span className="ck"><Icon name="check" /></span>
        </div>
      </button>
      {(onEdit || onDelete) && (
        <div className="st-tc-act">
          {onEdit && <button type="button" aria-label={t('appearance.edit')} onClick={onEdit}><Icon name="edit" /></button>}
          {onDelete && <button type="button" aria-label={t('appearance.delete')} onClick={onDelete}><Icon name="trash" /></button>}
        </div>
      )}
    </div>
  );
}

/* ---- colour helpers for the editor ---- */
const toRgb = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
const mix = (a: string, b: string, p: number) => '#' + toRgb(a).map((v, i) => Math.round(v * p + toRgb(b)[i] * (1 - p)).toString(16).padStart(2, '0')).join('').toUpperCase();
const isLight = (h: string) => { const [r, g, b] = toRgb(h); return 0.299 * r + 0.587 * g + 0.114 * b > 150; };

export function ThemeGrid() {
  const t = useT('settings');
  const th = useTheme();
  const [editing, setEditing] = useState<ThemeDef | null>(null);
  const savedRef = useRef(th.themeId);
  savedRef.current = th.themeId;

  const pick = (id: string, name: string) => {
    const snap = th.snapshot();
    th.setTheme(id);
    toast.undo(t('saved', { name: t('appearance.theme.title') + ': ' + name }), t('undo'), () => th.restore(snap));
  };
  const group = (kind: 'dark' | 'light') =>
    th.themes.filter((x) => x.kind === kind).map((x) => (
      <ThemeCard
        key={x.id}
        t={x}
        on={th.themeId === x.id}
        onPick={() => pick(x.id, x.name)}
        onEdit={x.custom ? () => setEditing(x) : undefined}
        onDelete={x.custom ? () => { th.deleteCustom(x.id); toast.info(t('appearance.deleted', { name: x.name })); } : undefined}
      />
    ));

  const startNew = () => {
    const b = th.theme;
    setEditing({ ...b, id: 'custom-new', name: t('appearance.myTheme'), custom: true, own: false, note: undefined });
  };

  const importFile = async (f: File | undefined) => {
    if (!f) return;
    try {
      const txt = await f.text();
      const imp = th.importTheme(txt);
      toast.ok(t('appearance.imported', { name: imp.name }));
    } catch {
      toast.err(t('appearance.importFailed'), t('appearance.importFailedHint'));
    }
  };
  const fileRef = useRef<HTMLInputElement>(null);

  return (
    <>
      <div className="st-gl2">{t('appearance.dark')}</div>
      <div className="st-tg">{group('dark')}</div>
      <div className="st-gl2">{t('appearance.light')}</div>
      <div className="st-tg">{group('light')}</div>
      {!editing && (
        <div style={{ display: 'flex', gap: 8, marginTop: 16, flexWrap: 'wrap' }}>
          <Button icon="palette" onClick={startNew}>{t('appearance.create')}</Button>
          <Button icon="upload" onClick={() => fileRef.current?.click()}>{t('appearance.import')}</Button>
          <input ref={fileRef} type="file" accept="application/json,.json" hidden onChange={(e) => { void importFile(e.target.files?.[0]); e.target.value = ''; }} />
        </div>
      )}
      {editing && <ThemeEditor key={editing.id} initial={editing} onDone={() => setEditing(null)} />}
    </>
  );
}

function ThemeEditor({ initial, onDone }: { initial: ThemeDef; onDone(): void }) {
  const t = useT('settings');
  const th = useTheme();
  const [d, setD] = useState<ThemeDef>(initial);
  const [name, setName] = useState(initial.name);

  // Live preview while editing; stop on close.
  useEffect(() => {
    th.preview({ ...d, name });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [d, name]);
  useEffect(() => () => th.preview(null), []); // eslint-disable-line react-hooks/exhaustive-deps

  const base = (k: 'bg' | 'surface' | 'sunk' | 'ink', v: string) =>
    setD((x) => {
      const n = { ...x, [k]: v } as ThemeDef;
      n.kind = isLight(n.bg) ? 'light' : 'dark';
      n.line = mix(n.ink, n.surface, 0.14);
      n.ink2 = mix(n.ink, n.bg, 0.68);
      n.ink3 = mix(n.ink, n.bg, 0.42);
      return n;
    });
  const hue = (k: HueKey, v: string) => setD((x) => ({ ...x, hues: { ...x.hues, [k]: v }, distro: k === 'ov' ? v : x.distro }));

  const finish = (def: ThemeDef) => {
    const parsed = parseTheme({ ...def, name: name.trim() || t('appearance.myTheme') });
    if (!parsed) return toast.err(t('appearance.invalid'));
    const saved = th.saveCustom({ ...parsed, id: initial.id });
    th.preview(null);
    th.setTheme(saved.id);
    toast.ok(t('appearance.savedTheme', { name: parsed.name }));
    onDone();
  };
  const exportJson = () => {
    const txt = th.exportTheme({ ...d, name });
    const url = URL.createObjectURL(new Blob([txt], { type: 'application/json' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = `${(name || 'theme').toLowerCase().replace(/[^a-z0-9]+/g, '-')}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };
  const field = (key: string, label: string, val: string, on: (v: string) => void) => (
    <label className="st-cf" key={key}>
      <input type="color" value={val} onChange={(e) => on(e.target.value.toUpperCase())} aria-label={label} />
      <div>{label}<small>{val.toUpperCase()}</small></div>
    </label>
  );

  return (
    <div className="st-ed">
      <div className="hd">
        <b>{initial.id === 'custom-new' ? t('appearance.createTitle') : t('appearance.editTitle')}</b>
        <span className="muted" style={{ fontSize: 13 }}>{t('appearance.createHint')}</span>
        <div className="r">
          <Button icon="download" onClick={exportJson}>{t('appearance.export')}</Button>
          <Button variant="ghost" onClick={onDone}>{t('cancel')}</Button>
          <Button variant="primary" onClick={() => finish(d)}>{t('appearance.saveAs', { name: name || t('appearance.myTheme') })}</Button>
        </div>
      </div>
      <Input label={t('appearance.name')} value={name} maxLength={40} onChange={(e) => setName(e.target.value)} />
      <div className="st-ed-lb">{t('appearance.base')}</div>
      <div className="st-cols2">
        {field('bg', t('appearance.c.bg'), d.bg, (v) => base('bg', v))}
        {field('surface', t('appearance.c.surface'), d.surface, (v) => base('surface', v))}
        {field('sunk', t('appearance.c.sunk'), d.sunk, (v) => base('sunk', v))}
        {field('ink', t('appearance.c.ink'), d.ink, (v) => base('ink', v))}
      </div>
      <div className="st-ed-lb">{t('appearance.sectionColours')}</div>
      <div className="st-cols2">
        {HUE_KEYS.map((k) => field(k, t(`appearance.h.${HUE_LABEL_KEYS[k]}`), d.hues[k], (v) => hue(k, v)))}
      </div>
    </div>
  );
}

export function ColourBlock() {
  const t = useT('settings');
  const th = useTheme();
  const { host } = useSession();
  const [chip, setChip] = useState<string | null>(null);
  const detected = host?.distro.name ?? 'Linux';
  useEffect(() => () => th.previewDistro(null), []); // eslint-disable-line react-hooks/exhaustive-deps
  const setMode = (m: ColourMode) => {
    const snap = th.snapshot();
    th.setColourMode(m);
    toast.undo(t('saved', { name: t('appearance.colours.title') }), t('undo'), () => th.restore(snap));
  };
  return (
    <>
      <Segmented<ColourMode>
        aria-label={t('appearance.colours.title')}
        value={th.colourMode}
        onChange={setMode}
        options={[
          { value: 'sections', label: t('appearance.colours.sections') },
          { value: 'distro', label: t('appearance.colours.distro') },
          { value: 'mono', label: t('appearance.colours.mono') },
        ]}
      />
      <div className={`st-drow${th.colourMode === 'distro' ? '' : ' dim'}`}>
        <span className="dl">{t('appearance.colours.detected')} <b>{detected}</b>. {t('appearance.colours.preview')}</span>
        {DISTRO_PRESETS.map((p) => (
          <button
            key={p.id}
            type="button"
            className="st-dc"
            aria-pressed={chip === p.id}
            onClick={() => {
              setChip(p.id);
              th.previewDistro(p.color);
              if (th.colourMode !== 'distro') th.setColourMode('distro');
            }}
          >
            <i style={{ background: p.color }} />
            {p.name}
          </button>
        ))}
        {chip && (
          <button type="button" className="st-dc" onClick={() => { setChip(null); th.previewDistro(null); }}>
            <Icon name="undo" size={14} />
            {t('appearance.colours.reset')}
          </button>
        )}
      </div>
    </>
  );
}

export { BUILTIN_THEMES };
