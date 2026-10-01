import { useState } from 'react';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, Dialog, IconButton, Input } from '../../ui';

export interface HostEntry {
  name: string;
  address: string;
  user?: string;
}

export function HostsBlock({ hosts, onChange, disabled, supported }: { hosts: HostEntry[]; onChange(next: HostEntry[], what: string): void; disabled?: boolean; supported: boolean }) {
  const t = useT('settings');
  const { host } = useSession();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [address, setAddress] = useState('');
  const [user, setUser] = useState('');
  const valid = name.trim() && address.trim();
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!valid) return;
    onChange([...hosts, { name: name.trim(), address: address.trim(), user: user.trim() || undefined }], name.trim());
    setAdding(false);
    setName(''); setAddress(''); setUser('');
  };
  return (
    <>
      <div className="st-host">
        <span className="dot" />
        <div className="grow"><b>{host?.hostname ?? t('hosts.thisMachine')}</b><small>{t('hosts.thisMachine')}{host?.ip ? `, ${host.ip}` : ''}</small></div>
        <Badge tone="ok">{t('hosts.online')}</Badge>
      </div>
      {hosts.map((h) => (
        <div className="st-host" key={h.name}>
          <span className="dot" style={{ background: 'var(--ink3)' }} />
          <div className="grow"><b>{h.name}</b><small>{h.user ? `${h.user}@` : ''}{h.address}</small></div>
          <IconButton icon="trash" label={t('hosts.remove', { name: h.name })} disabled={disabled} onClick={() => onChange(hosts.filter((x) => x !== h), h.name)} />
        </div>
      ))}
      {supported ? (
        <div style={{ padding: '10px 0' }}>
          <Button icon="plus" disabled={disabled} onClick={() => setAdding(true)}>{t('hosts.add')}</Button>
        </div>
      ) : (
        <div className="st-tx" style={{ padding: '6px 0 0' }}><small>{t('hosts.later')}</small></div>
      )}
      <Dialog
        open={adding}
        onClose={() => setAdding(false)}
        title={t('hosts.addTitle')}
        description={t('hosts.addText')}
        icon="server"
        onSubmit={submit}
        footer={<><Button variant="ghost" onClick={() => setAdding(false)}>{t('cancel')}</Button><Button type="submit" variant="primary" disabled={!valid}>{t('hosts.add')}</Button></>}
      >
        <Input data-autofocus label={t('hosts.name')} value={name} onChange={(e) => setName(e.target.value)} placeholder="backup-nas" />
        <Input label={t('hosts.address')} icon="server" mono value={address} onChange={(e) => setAddress(e.target.value)} placeholder="192.168.1.40" />
        <Input label={t('hosts.user')} icon="user" value={user} onChange={(e) => setUser(e.target.value)} placeholder="root" hint={t('hosts.userHint')} />
      </Dialog>
    </>
  );
}
