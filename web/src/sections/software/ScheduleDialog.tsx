import { useEffect, useState } from 'react';
import { ApiError, call, useSession } from '../../api';
import { useT } from '../../i18n';
import { Button, Dialog, Input, toast } from '../../ui';
import { loadSummary } from './store';

/** Schedule the nightly update: a systemd timer (ervisio-update.timer) that runs the same upgrade as "Update all". */
export function ScheduleDialog({ open, onClose, current }: { open: boolean; onClose(): void; current: string | null }) {
  const t = useT('software');
  const win = useSession().session?.os === 'windows';
  const [at, setAt] = useState('03:00');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  useEffect(() => {
    if (open) {
      setAt(current ?? '03:00');
      setErr('');
    }
  }, [open, current]);

  const save = async (value: string | null) => {
    setBusy(true);
    setErr('');
    try {
      await call('software.schedule', { at: value });
      await loadSummary();
      toast.ok(value ? t('schedule.saved', { time: value }) : t('schedule.removed'));
      onClose();
    } catch (e) {
      if (!(e instanceof ApiError && e.code === 'cancelled')) setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t('schedule.title')}
      description={t(win ? 'win.schedule.text' : 'schedule.text')}
      icon="clock"
      onSubmit={(e) => {
        e.preventDefault();
        void save(at);
      }}
      footer={
        <>
          {current && (
            <Button variant="ghost" disabled={busy} onClick={() => void save(null)}>{t('schedule.remove')}</Button>
          )}
          <Button onClick={onClose}>{t('cancel')}</Button>
          <Button variant="primary" type="submit" loading={busy}>{t('schedule.save')}</Button>
        </>
      }
    >
      <Input label={t('schedule.time')} type="time" value={at} onChange={(e) => setAt(e.target.value)} required error={err || undefined} hint={t('schedule.hint')} />
    </Dialog>
  );
}
