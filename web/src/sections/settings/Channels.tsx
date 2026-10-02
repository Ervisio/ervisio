import { useState } from 'react';
import { Name } from '../../brand';
import { useT } from '../../i18n';
import { formatDateTime } from '../../lib/format';
import { Badge, Button, Checkbox, ConfirmDialog, Dialog, Icon, IconButton, Input, Select, Switch, Textarea, toast } from '../../ui';
import { deleteChannel, listChannels, saveChannel, testChannel, type Channel, type ChannelEvent, type ChannelInput, type ChannelType } from './jobsApi';
import { LockedPanel, useAdminLoad } from './useAdminLoad';

const TYPES: ChannelType[] = ['email', 'telegram', 'webhook', 'ntfy', 'gotify'];
const EVENTS: ChannelEvent[] = ['alerts', 'updates', 'plugins', 'jobs'];
const DEFAULT_TEMPLATE = '{"title":"{title}","body":"{body}","level":"{level}","link":"{link}","source":"{source}","time":"{time}"}';
const PORTS = { starttls: 587, tls: 465, none: 25 } as const;

/** The form's state: every field of every type, as strings. Secrets start empty (empty = keep the stored one). */
interface Draft {
  id?: string;
  type: ChannelType;
  name: string;
  enabled: boolean;
  minLevel: 'info' | 'warn' | 'error';
  events: ChannelEvent[];
  host: string; port: string; security: 'none' | 'starttls' | 'tls'; username: string; password: string; from: string; to: string;
  botToken: string; chatId: string;
  url: string; method: string; template: string; headerName: string; headerValue: string;
  server: string; topic: string; token: string;
}

const blank = (type: ChannelType): Draft => ({
  type, name: '', enabled: true, minLevel: 'info', events: [...EVENTS],
  host: '', port: '587', security: 'starttls', username: '', password: '', from: '', to: '',
  botToken: '', chatId: '', url: '', method: 'POST', template: DEFAULT_TEMPLATE, headerName: '', headerValue: '',
  server: type === 'ntfy' ? 'https://ntfy.sh' : '', topic: '', token: '',
});

const fromChannel = (c: Channel): Draft => ({
  ...blank(c.type),
  id: c.id, name: c.name, enabled: c.enabled, minLevel: c.minLevel, events: c.events,
  host: c.host ?? '', port: String(c.port ?? 587), security: c.security ?? 'starttls', username: c.username ?? '', from: c.from ?? '', to: (c.to ?? []).join(', '),
  chatId: c.chatId ?? '', method: c.method ?? 'POST', template: c.template ?? DEFAULT_TEMPLATE, headerName: c.headerName ?? '',
  server: c.server ?? '', topic: c.topic ?? '',
});

function toInput(d: Draft): ChannelInput {
  const base = { id: d.id, name: d.name.trim(), type: d.type, enabled: d.enabled, minLevel: d.minLevel, events: d.events };
  switch (d.type) {
    case 'email':
      return { ...base, host: d.host.trim(), port: Number(d.port) || 0, security: d.security, username: d.username.trim(), password: d.password, from: d.from.trim(), to: d.to.split(/[\s,;]+/).filter(Boolean) };
    case 'telegram':
      return { ...base, botToken: d.botToken.trim(), chatId: d.chatId.trim() };
    case 'webhook':
      return { ...base, url: d.url.trim(), method: d.method, template: d.template, headerName: d.headerName.trim(), headerValue: d.headerValue };
    case 'ntfy':
      return { ...base, server: d.server.trim(), topic: d.topic.trim(), token: d.token.trim() };
    default:
      return { ...base, server: d.server.trim(), token: d.token.trim() };
  }
}

/** Settings > Notification channels (administrators): email, Telegram, webhook, ntfy and Gotify. */
export function ChannelsBlock() {
  const t = useT('settings');
  const { data, state, error, reload } = useAdminLoad(listChannels);
  const [editing, setEditing] = useState<Draft | null>(null);
  const [removing, setRemoving] = useState<Channel | null>(null);
  const [testing, setTesting] = useState('');

  if (state === 'locked') return <LockedPanel title={t('channels.lockedTitle')} text={t('channels.lockedText')} onUnlocked={() => void reload()} />;
  if (state === 'failed') return <div className="st-warn" role="alert"><Icon name="alert" /><div>{error}</div></div>;
  const channels = data?.channels ?? [];

  const test = async (c: Channel) => {
    setTesting(c.id);
    try {
      await testChannel(fromChannelInput(c));
      toast.ok(t('channels.testOk', { name: c.name }));
    } catch (e) {
      toast.err(t('channels.testFail', { name: c.name }), e instanceof Error ? e.message : undefined);
    } finally {
      setTesting('');
      void reload();
    }
  };
  const toggle = async (c: Channel, enabled: boolean) => {
    try {
      // The server keeps the stored secrets when they are left out.
      await saveChannel({ ...(fromChannelInput(c)), enabled });
      void reload();
    } catch (e) {
      toast.err(t('saveFailed', { name: c.name }), e instanceof Error ? e.message : undefined);
    }
  };

  return (
    <>
      <p className="st-note">{t('channels.intro', { name: Name })}</p>
      {channels.length === 0 && state === 'ready' && <div className="st-tx" style={{ padding: '6px 0' }}><small>{t('channels.empty')}</small></div>}
      {channels.map((c) => (
        <div className="st-job" key={c.id}>
          <div className="st-job-hd">
            <span className="dot" style={{ background: !c.enabled ? 'var(--ink3)' : c.last && !c.last.ok ? 'var(--err)' : 'var(--ok)' }} />
            <div className="grow">
              <b>{c.name}</b>
              <small>{t(`channels.types.${c.type}`)} · {detail(c, t)}</small>
            </div>
            <Switch aria-label={t('channels.enabled', { name: c.name })} checked={c.enabled} onChange={(v) => void toggle(c, v)} />
          </div>
          <div className="st-job-meta">
            {c.events.length === 0 ? <Badge>{t('channels.noEvents')}</Badge> : c.events.map((e) => <Badge key={e}>{t(`channels.ev.${e}`, { name: Name })}</Badge>)}
            {c.minLevel !== 'info' && <Badge tone="warn">{t(`channels.levels.${c.minLevel}`)}</Badge>}
          </div>
          {c.last && (
            <small className={`st-job-last${c.last.ok ? '' : ' st-job-last--err'}`}>
              {c.last.ok ? t('channels.lastOk', { when: formatDateTime(c.last.at) }) : t('channels.lastFail', { when: formatDateTime(c.last.at), error: c.last.error ?? '' })}
            </small>
          )}
          <div className="st-job-act">
            <Button icon="play" loading={testing === c.id} onClick={() => void test(c)}>{t('channels.test')}</Button>
            <Button icon="edit" onClick={() => setEditing(fromChannel(c))}>{t('channels.edit')}</Button>
            <IconButton icon="trash" label={t('channels.delete', { name: c.name })} onClick={() => setRemoving(c)} />
          </div>
        </div>
      ))}
      <div style={{ padding: '10px 0' }}>
        <Button icon="plus" disabled={state !== 'ready'} onClick={() => setEditing(blank('email'))}>{t('channels.add')}</Button>
      </div>
      <small className="st-note">{t('channels.file', { path: data?.file ?? '' })}</small>

      {editing && <ChannelDialog draft={editing} existing={channels.find((c) => c.id === editing.id)} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void reload(); }} />}
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t('channels.deleteTitle')}
        description={t('channels.deleteText', { name: removing?.name ?? '' })}
        confirmLabel={t('channels.deleteAction')}
        cancelLabel={t('cancel')}
        onConfirm={async () => { if (removing) { await deleteChannel(removing.id); void reload(); } }}
      />
    </>
  );
}

/** A channel's non-secret fields as an input (secrets omitted: the server keeps them). */
function fromChannelInput(c: Channel): ChannelInput {
  const d = fromChannel(c);
  return toInput(d);
}

function detail(c: Channel, t: (k: string, v?: Record<string, string | number>) => string): string {
  switch (c.type) {
    case 'email': return t('channels.detail.email', { host: c.host ?? '', port: c.port ?? 0, to: (c.to ?? []).join(', ') });
    case 'telegram': return t('channels.detail.telegram', { chat: c.chatId ?? '' });
    case 'webhook': return c.urlHint ?? '';
    case 'ntfy': return `${c.server ?? ''}/${c.topic ?? ''}`;
    default: return c.server ?? '';
  }
}

function ChannelDialog({ draft, existing, onClose, onSaved }: { draft: Draft; existing?: Channel; onClose(): void; onSaved(): void }) {
  const t = useT('settings');
  const [d, setD] = useState<Draft>(draft);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState<'' | 'save' | 'test'>('');
  const set = (patch: Partial<Draft>) => setD((x) => ({ ...x, ...patch }));
  const isNew = !d.id;
  const has = (k: string) => !!existing?.secrets?.[k];
  const secretProps = (k: string) => ({ type: 'password' as const, autoComplete: 'new-password', placeholder: has(k) ? t('channels.secretKept') : '' });

  const run = async (kind: 'save' | 'test') => {
    setBusy(kind);
    setErr('');
    try {
      if (kind === 'save') {
        await saveChannel(toInput(d));
        toast.ok(t('channels.saved', { name: d.name }));
        onSaved();
      } else {
        await testChannel(toInput(d));
        toast.ok(t('channels.testOk', { name: d.name || t(`channels.types.${d.type}`) }));
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy('');
    }
  };
  const toggleEvent = (e: ChannelEvent, on: boolean) => set({ events: on ? [...d.events, e] : d.events.filter((x) => x !== e) });

  return (
    <Dialog
      open
      onClose={onClose}
      size="lg"
      title={isNew ? t('channels.addTitle') : t('channels.editTitle', { name: existing?.name ?? '' })}
      description={t('channels.dialogText')}
      icon="bell"
      onSubmit={(e) => { e.preventDefault(); void run('save'); }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t('cancel')}</Button>
          <Button icon="play" loading={busy === 'test'} disabled={!!busy} onClick={() => void run('test')}>{t('channels.test')}</Button>
          <Button type="submit" variant="primary" loading={busy === 'save'} disabled={!!busy || !d.name.trim()}>{t('channels.save')}</Button>
        </>
      }
    >
      <div className="st-form">
        {isNew && (
          <Select label={t('channels.type')} value={d.type} options={TYPES.map((x) => ({ value: x, label: t(`channels.types.${x}`) }))} onChange={(v) => setD({ ...blank(v as ChannelType), name: d.name })} />
        )}
        <Input data-autofocus label={t('channels.name')} value={d.name} onChange={(e) => set({ name: e.target.value })} placeholder={t(`channels.types.${d.type}`)} />

        {d.type === 'email' && (
          <>
            <div className="st-form-row">
              <Input label={t('channels.email.host')} mono value={d.host} onChange={(e) => set({ host: e.target.value })} placeholder="smtp.example.org" />
              <Input label={t('channels.email.port')} mono inputMode="numeric" value={d.port} onChange={(e) => set({ port: e.target.value })} />
              <Select label={t('channels.email.security')} value={d.security} options={(['starttls', 'tls', 'none'] as const).map((x) => ({ value: x, label: t(`channels.email.sec.${x}`) }))}
                onChange={(v) => set({ security: v as Draft['security'], port: String(PORTS[v as keyof typeof PORTS] ?? d.port) })} />
            </div>
            <div className="st-form-row">
              <Input label={t('channels.email.user')} value={d.username} onChange={(e) => set({ username: e.target.value })} autoComplete="off" />
              <Input label={t('channels.email.password')} {...secretProps('password')} value={d.password} onChange={(e) => set({ password: e.target.value })} />
            </div>
            <Input label={t('channels.email.from')} value={d.from} onChange={(e) => set({ from: e.target.value })} placeholder="Ervisio <ervisio@example.org>" />
            <Input label={t('channels.email.to')} hint={t('channels.email.toHint')} value={d.to} onChange={(e) => set({ to: e.target.value })} placeholder="admin@example.org" />
          </>
        )}
        {d.type === 'telegram' && (
          <>
            <Input label={t('channels.telegram.token')} hint={t('channels.telegram.tokenHint')} mono {...secretProps('botToken')} value={d.botToken} onChange={(e) => set({ botToken: e.target.value })} />
            <Input label={t('channels.telegram.chat')} hint={t('channels.telegram.chatHint')} mono value={d.chatId} onChange={(e) => set({ chatId: e.target.value })} placeholder="-1001234567890" />
          </>
        )}
        {d.type === 'webhook' && (
          <>
            <Input label={t('channels.webhook.url')} hint={existing?.urlHint ? t('channels.webhook.urlSet', { url: existing.urlHint }) : t('channels.webhook.urlHint')} mono {...secretProps('url')} type="text"
              value={d.url} onChange={(e) => set({ url: e.target.value })} placeholder={has('url') ? t('channels.secretKept') : 'https://example.org/hook'} />
            <Select label={t('channels.webhook.method')} value={d.method} options={['POST', 'PUT', 'PATCH'].map((x) => ({ value: x, label: x }))} onChange={(v) => set({ method: v })} />
            <Textarea label={t('channels.webhook.template')} hint={t('channels.webhook.templateHint')} mono rows={3} value={d.template} onChange={(e) => set({ template: e.target.value })} />
            <div className="st-form-row">
              <Input label={t('channels.webhook.headerName')} mono value={d.headerName} onChange={(e) => set({ headerName: e.target.value })} placeholder="X-Token" />
              <Input label={t('channels.webhook.headerValue')} {...secretProps('headerValue')} value={d.headerValue} onChange={(e) => set({ headerValue: e.target.value })} />
            </div>
          </>
        )}
        {d.type === 'ntfy' && (
          <>
            <Input label={t('channels.ntfy.server')} mono value={d.server} onChange={(e) => set({ server: e.target.value })} />
            <Input label={t('channels.ntfy.topic')} mono value={d.topic} onChange={(e) => set({ topic: e.target.value })} placeholder="ervisio-alerts" />
            <Input label={t('channels.ntfy.token')} mono {...secretProps('token')} value={d.token} onChange={(e) => set({ token: e.target.value })} />
          </>
        )}
        {d.type === 'gotify' && (
          <>
            <Input label={t('channels.gotify.server')} mono value={d.server} onChange={(e) => set({ server: e.target.value })} placeholder="https://gotify.example.org" />
            <Input label={t('channels.gotify.token')} mono {...secretProps('token')} value={d.token} onChange={(e) => set({ token: e.target.value })} />
          </>
        )}

        <Select label={t('channels.minLevel')} value={d.minLevel} options={(['info', 'warn', 'error'] as const).map((x) => ({ value: x, label: t(`channels.levels.${x}`) }))} onChange={(v) => set({ minLevel: v as Draft['minLevel'] })} />
        <fieldset className="st-form-set">
          <legend>{t('channels.events')}</legend>
          {EVENTS.map((e) => <Checkbox key={e} checked={d.events.includes(e)} onChange={(on) => toggleEvent(e, on)} label={t(`channels.ev.${e}`, { name: Name })} />)}
        </fieldset>
        <Switch aria-label={t('channels.enabled', { name: d.name })} checked={d.enabled} onChange={(v) => set({ enabled: v })} label={t('channels.enabledShort')} />
        {err && <div className="st-warn" role="alert"><Icon name="alert" /><div>{err}</div></div>}
      </div>
    </Dialog>
  );
}
