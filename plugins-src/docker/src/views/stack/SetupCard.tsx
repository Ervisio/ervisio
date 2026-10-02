import { useState } from 'react';
import { ensureStacksFolder, STACKS_DIR } from '../../api/compose';
import { t } from '../../i18n';
import { Button, Icon, toast } from '../../kit';

/** One-time setup: /opt/stacks does not exist. Creating it needs the admin unlock (the broker asks for it). */
export function SetupCard({ onDone }: { onDone(): void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const go = async () => {
    setBusy(true);
    setErr('');
    try {
      await ensureStacksFolder();
      toast.ok(t('stacks.setup.done', { dir: STACKS_DIR }));
      onDone();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="dk-sk-setup">
      <span className="dk-sk-setup-ic"><Icon name="files" /></span>
      <div className="dk-sk-setup-tx">
        <b>{t('stacks.setup.title')}</b>
        <p>{t('stacks.setup.text', { dir: STACKS_DIR })}</p>
        <code className="dk-tag">install -d -m 2775 -g docker {STACKS_DIR}</code>
        {err && <p className="dk-sk-err">{err}</p>}
      </div>
      <Button variant="primary" icon="plus" loading={busy} onClick={go}>{t('stacks.setup.create')}</Button>
    </div>
  );
}
