import { useEffect, useState } from 'react';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { Button, Dialog, Input, Switch, Textarea } from '../../ui';
import type { Host, Snippet } from './types';

export function SnippetDialog({ open, initial, onClose, onSave }: { open: boolean; initial?: Partial<Snippet>; onClose(): void; onSave(title: string, command: string): void }) {
  const t = useT('terminal');
  const win = useSession().session?.os === 'windows';
  const [title, setTitle] = useState('');
  const [cmd, setCmd] = useState('');
  useEffect(() => {
    if (open) {
      setTitle(initial?.title ?? '');
      setCmd(initial?.command ?? '');
    }
  }, [open, initial]);
  const ok = title.trim() && cmd.trim();
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={initial?.id ? t('snippet.editTitle') : t('snippet.saveTitle')}
      description={t('snippet.desc')}
      icon="command"
      onSubmit={(e) => {
        e.preventDefault();
        if (ok) onSave(title.trim(), cmd.replace(/\r?\n+$/g, ''));
      }}
      footer={
        <>
          <Button onClick={onClose}>{t('cancel')}</Button>
          <Button type="submit" variant="primary" disabled={!ok}>
            {t('snippet.save')}
          </Button>
        </>
      }
    >
      <div className="terminal-form">
        <Input label={t('snippet.name')} value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t('snippet.namePh')} data-autofocus maxLength={60} />
        <Textarea label={t('snippet.command')} mono rows={3} value={cmd} onChange={(e) => setCmd(e.target.value)} placeholder={win ? "Get-Service | Where-Object Status -eq 'Stopped'" : "sudo systemctl restart nginx"} hint={t('snippet.hint')} />
      </div>
    </Dialog>
  );
}

export function RenameDialog({ open, name, onClose, onSave }: { open: boolean; name: string; onClose(): void; onSave(name: string): void }) {
  const t = useT('terminal');
  const [v, setV] = useState(name);
  useEffect(() => {
    if (open) setV(name);
  }, [open, name]);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t('renameTitle')}
      icon="edit"
      onSubmit={(e) => {
        e.preventDefault();
        if (v.trim()) onSave(v.trim());
      }}
      footer={
        <>
          <Button onClick={onClose}>{t('cancel')}</Button>
          <Button type="submit" variant="primary" disabled={!v.trim()}>
            {t('rename')}
          </Button>
        </>
      }
    >
      <div className="terminal-form">
        <Input label={t('sessionName')} value={v} onChange={(e) => setV(e.target.value)} data-autofocus maxLength={64} />
      </div>
    </Dialog>
  );
}

/** Connect to an SSH host (optionally saving it) or edit a saved host. */
export function HostDialog({ open, mode, initial, onClose, onSubmit }: { open: boolean; mode: 'connect' | 'edit'; initial?: Host | null; onClose(): void; onSubmit(h: Omit<Host, 'id'> & { id?: string }, save: boolean): void }) {
  const t = useT('terminal');
  const [name, setName] = useState('');
  const [host, setHost] = useState('');
  const [user, setUser] = useState('');
  const [port, setPort] = useState('');
  const [save, setSave] = useState(false);
  const [err, setErr] = useState('');
  useEffect(() => {
    if (!open) return;
    setName(initial?.name ?? '');
    setHost(initial?.host ?? '');
    setUser(initial?.user ?? '');
    setPort(initial?.port ? String(initial.port) : '');
    setSave(mode === 'edit');
    setErr('');
  }, [open, initial, mode]);
  const submit = () => {
    const h = host.trim();
    if (!/^[A-Za-z0-9](?:[A-Za-z0-9.:_-]{0,251}[A-Za-z0-9])?$/.test(h)) return setErr(t('host.badHost'));
    const u = user.trim();
    if (u && !/^[A-Za-z_][A-Za-z0-9_.-]{0,31}\$?$/.test(u)) return setErr(t('host.badUser'));
    const pn = port.trim() ? Number(port) : undefined;
    if (pn !== undefined && (!Number.isInteger(pn) || pn < 1 || pn > 65535)) return setErr(t('host.badPort'));
    onSubmit({ id: initial?.id, name: name.trim() || h, host: h, user: u || undefined, port: pn }, save);
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={mode === 'edit' ? (initial ? t('host.editTitle') : t('host.addTitle')) : t('host.connectTitle')}
      description={t('host.desc')}
      icon="server"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      footer={
        <>
          <Button onClick={onClose}>{t('cancel')}</Button>
          <Button type="submit" variant="primary">
            {mode === 'edit' ? t('host.save') : t('connect')}
          </Button>
        </>
      }
    >
      <div className="terminal-form">
        <Input label={t('host.host')} value={host} onChange={(e) => setHost(e.target.value)} placeholder="web-01.example.com" mono data-autofocus error={err || undefined} />
        <div className="terminal-form-row">
          <Input label={t('host.user')} value={user} onChange={(e) => setUser(e.target.value)} placeholder={t('host.userPh')} mono />
          <Input label={t('host.port')} value={port} onChange={(e) => setPort(e.target.value)} placeholder="22" inputMode="numeric" mono />
        </div>
        <Input label={t('host.name')} value={name} onChange={(e) => setName(e.target.value)} placeholder={t('host.namePh')} />
        {mode === 'connect' && (
          <Switch checked={save} onChange={setSave} label={t('host.saveToo')} />
        )}
      </div>
    </Dialog>
  );
}
