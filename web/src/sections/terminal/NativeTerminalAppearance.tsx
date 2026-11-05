import { useEffect, useState } from 'react';
import { errorMessage, usePrefs } from '../../api';
import { useT } from '../../i18n';
import { Button, Dialog, Icon, Input, Select, Switch } from '../../ui';
import { nativeAppearance, TERMINAL_FONTS, type NativeAppearance } from './appearance';

export function NativeTerminalAppearance({ open, onClose, onPreview }: {
  open: boolean;
  onClose(): void;
  onPreview(value: NativeAppearance | null): void;
}) {
  const t = useT('terminal');
  const { prefs, set } = usePrefs();
  const [draft, setDraft] = useState(() => nativeAppearance(prefs.terminal ?? {}));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!open) return;
    setDraft(nativeAppearance(prefs.terminal ?? {}));
    setError('');
    // A fresh draft is captured when the dialog opens, not on every live preview.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => {
    onPreview(open ? draft : null);
  }, [open, draft, onPreview]);
  const change = (patch: Partial<NativeAppearance>) => setDraft((value) => ({ ...value, ...patch }));
  const close = () => { if (!saving) onClose(); };
  const apply = async () => {
    setSaving(true); setError('');
    try {
      await set('terminal', { ...(prefs.terminal ?? {}), ...draft });
      onClose();
    } catch (e) { setError(errorMessage(e)); }
    finally { setSaving(false); }
  };
  return <Dialog open={open} onClose={close} dismissable={!saving} title={t('native.appearance')} description={t('native.appearanceHint')} icon="palette"
    footer={<><Button disabled={saving} onClick={close}>{t('cancel')}</Button><Button variant="primary" loading={saving} onClick={() => void apply()}>{t('native.apply')}</Button></>}>
    <div className="terminal-appearance">
      <div className="terminal-appearance-range"><label htmlFor="term-opacity"><Icon name="columns" />{t('native.opacity')}<output>{draft.backgroundOpacity}%</output></label><input id="term-opacity" type="range" min={0} max={100} value={draft.backgroundOpacity} onChange={(e) => change({ backgroundOpacity: Number(e.target.value) })} /></div>
      <div className="terminal-appearance-range"><label htmlFor="term-blur"><Icon name="palette" />{t('native.blur')}<output>{draft.backgroundBlur}px</output></label><input id="term-blur" type="range" min={0} max={32} value={draft.backgroundBlur} onChange={(e) => change({ backgroundBlur: Number(e.target.value) })} /></div>
      <div className="terminal-appearance-grid">
        <Select label={t('native.font')} value={draft.fontFamily} options={TERMINAL_FONTS.map((font) => ({ value: font, label: font }))} onChange={(fontFamily) => change({ fontFamily })} />
        <Input label={t('native.fontSize')} type="number" min={9} max={32} value={draft.fontSize} onChange={(e) => change({ fontSize: Math.max(9, Math.min(32, Number(e.target.value) || 14)) })} />
        <Select label={t('native.palette')} value={draft.theme} options={(['app', 'dark', 'light'] as const).map((theme) => ({ value: theme, label: t(`native.theme_${theme}`) }))} onChange={(theme) => change({ theme: theme as NativeAppearance['theme'] })} />
        <Select label={t('native.cursor')} value={draft.cursorStyle} options={(['block', 'bar', 'underline'] as const).map((cursor) => ({ value: cursor, label: t(`native.cursor_${cursor}`) }))} onChange={(cursorStyle) => change({ cursorStyle: cursorStyle as NativeAppearance['cursorStyle'] })} />
        <Input label={t('native.lineHeight')} type="number" min={1} max={2} step={0.05} value={draft.lineHeight} onChange={(e) => change({ lineHeight: Math.max(1, Math.min(2, Number(e.target.value) || 1.15)) })} />
        <Input label={t('native.scrollback')} type="number" min={500} max={50000} step={500} value={draft.scrollback} onChange={(e) => change({ scrollback: Math.max(500, Math.min(50000, Number(e.target.value) || 5000)) })} />
      </div>
      <Switch checked={draft.cursorBlink} onChange={(cursorBlink) => change({ cursorBlink })} label={t('native.cursorBlink')} />
      {error && <p className="terminal-appearance-error" role="alert">{error}</p>}
    </div>
  </Dialog>;
}
