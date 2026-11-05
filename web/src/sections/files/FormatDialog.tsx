import { useEffect, useRef, useState } from 'react';
import { call, errorMessage, stream, type StreamHandle } from '../../api';
import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import type { WindowCloseRequest } from '../../lib/windowClose';
import { Button, Checkbox, Dialog, Icon, Input, Progress, Select, Skeleton, toast } from '../../ui';
import type { Disk } from './types';

interface Volume {
  device: string;
  name: string;
  size: number;
  fstype: string;
  label: string;
  mounts: string[];
  blocked?: string;
  identity: string;
  confirmation: string;
  filesystems: string[];
  platform: string;
}
interface FormatEvent {
  phase: string;
  percent?: number;
  message?: string;
}
export function FormatDialog({
  disk,
  onClose,
  onFormatted,
}: {
  disk: Disk;
  onClose(): void;
  onFormatted(): void;
}) {
  const t = useT('storage');
  const [volume, setVolume] = useState<Volume | null>(null);
  const [filesystem, setFilesystem] = useState('');
  const [label, setLabel] = useState('');
  const [quick, setQuick] = useState(true);
  const [force, setForce] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const [event, setEvent] = useState<FormatEvent | null>(null);
  const handle = useRef<StreamHandle | null>(null);
  const owner = useRef(
    document.querySelector<HTMLElement>('.eos-window.is-active')?.dataset.windowId,
  );
  useEffect(() => {
    let cancelled = false;
    void call<Volume>('storage.inspect', { device: disk.device }).then(
      (device) => {
        if (cancelled) return;
        setVolume(device);
        setLabel(device.label);
        setFilesystem(
          device.filesystems.includes(device.fstype.toLowerCase())
            ? device.fstype.toLowerCase()
            : device.filesystems[0] || '',
        );
      },
      (e) => {
        if (!cancelled) setError(errorMessage(e));
      },
    );
    return () => {
      cancelled = true;
      handle.current?.close();
    };
  }, [disk.device]);
  useEffect(() => {
    if (!busy) return;
    const close = (e: Event) => {
      if ((e as CustomEvent<WindowCloseRequest>).detail.id === owner.current) {
        e.preventDefault();
        toast.info(t('running'));
      }
    };
    const unload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('ervisio:before-window-close', close);
    window.addEventListener('beforeunload', unload);
    return () => {
      window.removeEventListener('ervisio:before-window-close', close);
      window.removeEventListener('beforeunload', unload);
    };
  }, [busy, t]);
  const submit = () => {
    if (!volume || volume.blocked || busy || done || confirmation !== volume.confirmation) return;
    setBusy(true);
    setError('');
    setEvent({ phase: 'check' });
    handle.current = stream<FormatEvent>(
      'storage.format',
      {
        device: volume.device,
        identity: volume.identity,
        filesystem,
        label,
        quick,
        force,
        confirm: confirmation,
      },
      {
        admin: true,
        onData: setEvent,
        onEnd: () => {
          setBusy(false);
          setDone(true);
          setEvent({ phase: 'done', percent: 100 });
          onFormatted();
        },
        onError: (e) => {
          setBusy(false);
          setError(errorMessage(e));
        },
      },
    );
  };
  return (
    <Dialog
      open
      size="md"
      onClose={() => !busy && onClose()}
      dismissable={!busy}
      title={t('title', { name: disk.mount || disk.device })}
      icon="disk"
      tone="warn"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            {t(done ? 'close' : 'cancel')}
          </Button>
          {!done && (
            <Button
              type="submit"
              variant="danger-solid"
              loading={busy}
              disabled={
                !volume || !!volume.blocked || !filesystem || confirmation !== volume.confirmation
              }
            >
              {t('start')}
            </Button>
          )}
        </>
      }
    >
      {!volume && !error && <Skeleton lines={5} />}
      {volume && (
        <div style={{ display: 'grid', gap: 14 }}>
          <div className="storage-volume-info">
            <b>{volume.device}</b>
            <span>{formatBytes(volume.size)}</span>
            <small>{volume.mounts.join(', ') || t('unmounted')}</small>
          </div>
          {volume.blocked ? (
            <div role="status" className="ui-form-err">
              <Icon name="shield" />
              <span>{t(`blocked.${volume.blocked}`)}</span>
            </div>
          ) : (
            <>
              <Select
                label={t('filesystem')}
                value={filesystem}
                disabled={busy || done}
                options={volume.filesystems.map((id) => ({ value: id, label: id.toUpperCase() }))}
            onChange={setFilesystem}
              />
              <Input
                label={t('label')}
                value={label}
                disabled={busy || done}
                maxLength={32}
                onChange={(e) => setLabel(e.target.value)}
                autoComplete="off"
              />
              <Checkbox
                label={t('quick')}
                checked={quick}
                disabled={busy || done}
                onChange={setQuick}
              />
              <small className="files-muted">{t(quick ? 'quickHint' : 'fullHint')}</small>
              <Checkbox
                label={t(volume.platform === 'windows' ? 'forceWindows' : 'unmount')}
                checked={force}
                disabled={busy || done}
                onChange={setForce}
              />
              <small className="files-muted">{t('forceHint')}</small>
              {!done && (
                <>
                  <p className="ui-form-err">
                    <Icon name="alert" />
                    {t('warning')}
                  </p>
                  <Input
                    label={t('confirm', { text: volume.confirmation })}
                    value={confirmation}
                    disabled={busy}
                    onChange={(e) => setConfirmation(e.target.value)}
                    mono
                    autoComplete="off"
                    spellCheck={false}
                  />
                </>
              )}
            </>
          )}
          {event && (
            <div role="status">
              <p>{t(`phase.${event.phase}`)}</p>
              {typeof event.percent === 'number' && <Progress value={event.percent} hue="file" />}
              {event.message && <small className="mono">{event.message}</small>}
            </div>
          )}
        </div>
      )}
      {error && (
        <div className="ui-form-err" role="alert">
          <Icon name="alert" />
          {error}
        </div>
      )}
    </Dialog>
  );
}
