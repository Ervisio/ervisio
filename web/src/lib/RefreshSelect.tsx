import { useState } from 'react';
import { useT } from '../i18n';
import { Button, Dialog, Input, Select } from '../ui';
import { REFRESH_MAX, REFRESH_MIN, REFRESH_OPTIONS, clampRefresh, formatInterval, useRefreshInterval } from './refresh';

const CUSTOM = 'custom';

/** Select for the refresh interval with a "Custom…" option. `onPick` receives the new value in ms. */
export function RefreshSelect({ onPick, compact = true, label }: { onPick(ms: number): void; compact?: boolean; label: string }) {
  const t = useT('common');
  const value = useRefreshInterval();
  const preset = (REFRESH_OPTIONS as readonly number[]).includes(value);
  const [open, setOpen] = useState(false);
  const [num, setNum] = useState('');
  const [unit, setUnit] = useState<'s' | 'min'>('s');
  const factor = unit === 'min' ? 60_000 : 1000;
  const n = Number(num.replace(',', '.'));
  const ms = n * factor;
  const valid = num.trim() !== '' && Number.isFinite(n) && ms >= REFRESH_MIN && ms <= REFRESH_MAX;

  const options = [
    ...REFRESH_OPTIONS.map((o) => ({ value: String(o), label: formatInterval(o) })),
    ...(preset ? [] : [{ value: String(value), label: t('refreshRate.customValue', { value: formatInterval(value) }) }]),
    { value: CUSTOM, label: t('refreshRate.custom') },
  ];
  const change = (v: string) => {
    if (v === CUSTOM) {
      const useMin = value >= 60_000 && value % 60_000 === 0;
      setUnit(useMin ? 'min' : 's');
      setNum(String(useMin ? value / 60_000 : value / 1000));
      setOpen(true);
    } else onPick(clampRefresh(Number(v)));
  };
  return (
    <>
      <Select compact={compact} aria-label={label} value={String(value)} options={options} onChange={change} />
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={t('refreshRate.customTitle')}
        description={t('refreshRate.customDesc')}
        icon="refresh"
        onSubmit={(e) => {
          e.preventDefault();
          if (!valid) return;
          onPick(clampRefresh(ms));
          setOpen(false);
        }}
        footer={
          <>
            <Button variant="ghost" type="button" onClick={() => setOpen(false)}>{t('cancel')}</Button>
            <Button variant="primary" type="submit" disabled={!valid}>{t('save')}</Button>
          </>
        }
      >
        <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
          <Input
            label={t('refreshRate.every')}
            type="number"
            inputMode="decimal"
            min={unit === 'min' ? REFRESH_MIN / 60_000 : 1}
            max={unit === 'min' ? 10 : 600}
            step="any"
            autoFocus
            value={num}
            onChange={(e) => setNum(e.target.value)}
            error={num !== '' && !valid ? t('refreshRate.invalid') : undefined}
          />
          <Select label={t('refreshRate.unit')} value={unit} onChange={(v) => setUnit(v as 's' | 'min')} options={[{ value: 's', label: t('refreshRate.seconds') }, { value: 'min', label: t('refreshRate.minutes') }]} />
        </div>
      </Dialog>
    </>
  );
}
