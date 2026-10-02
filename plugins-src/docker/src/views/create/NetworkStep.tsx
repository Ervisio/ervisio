import { useState } from 'react';
import { networks } from '../../api/resources';
import { t } from '../../i18n';
import { Button, Chip, IconButton, Input, Select, Switch } from '../../kit';
import { Hint, removeAt, replaceAt, Section, type StepProps } from './parts';
import { kv, type Problems } from './model';

type Adv = 'labels' | 'command' | 'user' | 'privileged';

export function NetworkStep({ spec, patch, showErrors, problems }: StepProps & { problems: Problems }) {
  const { data: nets } = networks.use();
  const [open, setOpen] = useState<Set<Adv>>(() => {
    const s = new Set<Adv>();
    if (spec.labels.length) s.add('labels');
    if (spec.command) s.add('command');
    if (spec.user) s.add('user');
    if (spec.privileged) s.add('privileged');
    return s;
  });
  const toggle = (a: Adv) => setOpen((s) => { const n = new Set(s); if (n.has(a)) n.delete(a); else n.add(a); return n; });

  const names = new Set<string>(['bridge', 'host', 'none']);
  for (const n of nets ?? []) names.add(n.Name);
  if (spec.network) names.add(spec.network);
  const options = [...names].sort((a, b) => (a === 'bridge' ? -1 : b === 'bridge' ? 1 : a.localeCompare(b))).map((n) => ({
    value: n,
    label: n === 'bridge' ? t('create.net.bridge') : n === 'host' ? t('create.net.host') : n === 'none' ? t('create.net.none') : n,
  }));
  const hostMode = spec.network === 'host';

  return (
    <>
      <Section title={t('create.network')}>
        <div className="dk-cr-g2">
          <Select label={t('create.network')} value={spec.network} onChange={(v) => patch({ network: v })} options={options} hint={hostMode ? t('create.net.hostHint') : t('create.net.hint')} />
          <Input label={t('create.hostname')} mono value={spec.hostname} disabled={hostMode} onChange={(e) => patch({ hostname: e.target.value })} placeholder={t('create.hostname.ph')} spellCheck={false} autoCapitalize="off" />
        </div>
      </Section>

      <Section title={t('create.limits')}>
        <div className="dk-cr-g2">
          <Input label={t('create.memory')} mono inputMode="decimal" value={spec.memoryMb} onChange={(e) => patch({ memoryMb: e.target.value })} placeholder="512" end={<span className="dk-muted">MB</span>} hint={t('create.limit.hint')} />
          <Input label={t('create.cpus')} mono inputMode="decimal" value={spec.cpus} onChange={(e) => patch({ cpus: e.target.value })} placeholder="1.0" hint={t('create.cpus.hint')} />
        </div>
        {showErrors && problems.limits && <Hint tone="err" icon="alert">{t(problems.limits)}</Hint>}
      </Section>

      <Section title={t('create.advanced')} note={t('create.optional')}>
        <div className="dk-cr-chips" role="group" aria-label={t('create.advanced')}>
          {(['labels', 'command', 'user', 'privileged'] as Adv[]).map((a) => (
            <Chip key={a} pressed={open.has(a)} onClick={() => toggle(a)}>{t(`create.adv.${a}`)}</Chip>
          ))}
        </div>
        {open.has('labels') && (
          <div className="dk-cr-sub">
            <b>{t('create.adv.labels')}</b>
            {spec.labels.map((l, i) => (
              <div className="dk-cr-pr" key={l.id}>
                <Input mono compact value={l.key} placeholder="traefik.enable" aria-label={t('create.env.key')} onChange={(e) => patch({ labels: replaceAt(spec.labels, i, { key: e.target.value }) })} fieldClassName="dk-cr-fld" spellCheck={false} />
                <Input mono compact value={l.value} placeholder="true" aria-label={t('create.env.value')} onChange={(e) => patch({ labels: replaceAt(spec.labels, i, { value: e.target.value }) })} fieldClassName="dk-cr-fld" spellCheck={false} />
                <IconButton icon="trash" label={t('common.remove')} onClick={() => patch({ labels: removeAt(spec.labels, i) })} />
              </div>
            ))}
            <Button className="dk-cr-add" variant="ghost" size="sm" icon="plus" onClick={() => patch({ labels: [...spec.labels, kv()] })}>{t('create.label.add')}</Button>
          </div>
        )}
        {open.has('command') && (
          <div className="dk-cr-sub">
            <Input label={t('create.adv.command')} mono value={spec.command} onChange={(e) => patch({ command: e.target.value })} placeholder="--port 8080 --verbose" hint={t('create.command.hint')} spellCheck={false} />
          </div>
        )}
        {open.has('user') && (
          <div className="dk-cr-sub">
            <Input label={t('create.adv.user')} mono value={spec.user} onChange={(e) => patch({ user: e.target.value })} placeholder="1000:1000" hint={t('create.user.hint')} spellCheck={false} />
          </div>
        )}
        {open.has('privileged') && (
          <div className="dk-cr-sub">
            <Switch checked={spec.privileged} onChange={(v) => patch({ privileged: v })} label={t('create.privileged')} />
            {spec.privileged && <Hint tone="warn" icon="alert">{t('create.privileged.warn')}</Hint>}
          </div>
        )}
      </Section>
    </>
  );
}
