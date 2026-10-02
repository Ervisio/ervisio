import { useMemo } from 'react';
import { containers, volumes } from '../../api/resources';
import { t } from '../../i18n';
import { Button, Checkbox, IconButton, Icon, Input, Select } from '../../kit';
import { aria, Hint, removeAt, replaceAt, Section, type StepProps } from './parts';
import { NO_PORTS_MODES, portIssue, rid, usedHostPorts, validPort, type Problems } from './model';

export function StorageStep({ spec, patch, showErrors, problems, from }: StepProps & { problems: Problems; from?: string }) {
  const { data: list } = containers.use();
  const { data: vols } = volumes.use();
  const used = useMemo(() => usedHostPorts(list, from), [list, from]);
  const noPorts = NO_PORTS_MODES(spec.network);

  const issues = spec.ports.map((_, i) => portIssue(spec.ports, i, used));
  const conflicts = issues.map((x, i) => (x?.kind === 'conflict' ? { port: spec.ports[i].host.trim().replace(/^.*:/, ''), by: x.by! } : null)).filter((x): x is { port: string; by: string } => !!x);
  const hostPorts = spec.ports.filter((p) => p.host.trim()).length;

  return (
    <>
      <Section title={t('create.ports')} note={t('create.ports.note')}>
        {noPorts && <Hint tone="warn" icon="alert">{t('create.ports.ignored', { mode: spec.network })}</Hint>}
        {spec.ports.map((p, i) => {
          const bad = issues[i]?.kind === 'invalid' || issues[i]?.kind === 'duplicate' || (showErrors && !p.container.trim() && !!p.host.trim());
          return (
            <div className="dk-cr-pr" key={p.id}>
              <Input mono compact value={p.host} placeholder={t('create.port.random')} aria-label={t('create.port.host')} className={bad ? 'dk-cr-bad' : undefined} onChange={(e) => patch({ ports: replaceAt(spec.ports, i, { host: e.target.value }) })} fieldClassName="dk-cr-fld" />
              <span className="dk-cr-ar"><Icon name="right" size={14} /></span>
              <Input mono compact value={p.container} placeholder="80" aria-label={t('create.port.container')} className={bad && !validPort(p.container.trim()) ? 'dk-cr-bad' : undefined} onChange={(e) => patch({ ports: replaceAt(spec.ports, i, { container: e.target.value }) })} fieldClassName="dk-cr-fld" />
              <div className="dk-cr-proto"><Select compact value={p.proto} onChange={(v) => patch({ ports: replaceAt(spec.ports, i, { proto: v as 'tcp' | 'udp' }) })} options={[{ value: 'tcp', label: 'TCP' }, { value: 'udp', label: 'UDP' }]} {...aria(t('create.port.proto'))} /></div>
              <IconButton icon="trash" label={t('common.remove')} onClick={() => patch({ ports: removeAt(spec.ports, i) })} />
            </div>
          );
        })}
        {showErrors && problems.ports && <Hint tone="err" icon="alert">{t(problems.ports)}</Hint>}
        {conflicts.map((c) => (
          <Hint key={c.port} tone="warn" icon="alert">{t('create.port.conflict', { port: c.port, name: c.by })}</Hint>
        ))}
        {!conflicts.length && hostPorts > 0 && !problems.ports && !noPorts && <Hint tone="ok" icon="check">{t('create.port.free')}</Hint>}
        <Button className="dk-cr-add" variant="ghost" size="sm" icon="plus" onClick={() => patch({ ports: [...spec.ports, { id: rid(), host: '', container: '', proto: 'tcp' }] })}>{t('create.port.add')}</Button>
      </Section>

      <Section title={t('create.storage')} note={t('create.storage.note')}>
        <datalist id="dk-cr-vols">{(vols ?? []).map((v) => <option key={v.Name} value={v.Name} />)}</datalist>
        {spec.mounts.map((m, i) => (
          <div className="dk-cr-pr dk-cr-mount" key={m.id}>
            <div className="dk-cr-kind"><Select compact value={m.kind} onChange={(v) => patch({ mounts: replaceAt(spec.mounts, i, { kind: v as 'volume' | 'bind' }) })} options={[{ value: 'volume', label: t('create.mount.volume') }, { value: 'bind', label: t('create.mount.bind') }]} {...aria(t('create.mount.kind'))} /></div>
            <Input mono compact list={m.kind === 'volume' ? 'dk-cr-vols' : undefined} value={m.source} placeholder={m.kind === 'volume' ? t('create.mount.volume.ph') : '/srv/app/data'} aria-label={t('create.mount.source')} onChange={(e) => patch({ mounts: replaceAt(spec.mounts, i, { source: e.target.value }) })} fieldClassName="dk-cr-fld" spellCheck={false} />
            <span className="dk-cr-ar"><Icon name="right" size={14} /></span>
            <Input mono compact value={m.target} placeholder="/data" aria-label={t('create.mount.target')} onChange={(e) => patch({ mounts: replaceAt(spec.mounts, i, { target: e.target.value }) })} fieldClassName="dk-cr-fld" spellCheck={false} />
            <Checkbox checked={m.ro} onChange={(v) => patch({ mounts: replaceAt(spec.mounts, i, { ro: v }) })} aria-label={t('create.mount.ro')} label={t('create.mount.ro')} />
            <IconButton icon="trash" label={t('common.remove')} onClick={() => patch({ mounts: removeAt(spec.mounts, i) })} />
          </div>
        ))}
        {showErrors && problems.mounts && <Hint tone="err" icon="alert">{t(problems.mounts)}</Hint>}
        {spec.mounts.some((m) => m.kind === 'volume' && m.source.trim() && !(vols ?? []).some((v) => v.Name === m.source.trim())) && <Hint icon="info">{t('create.mount.newVolume')}</Hint>}
        <Button className="dk-cr-add" variant="ghost" size="sm" icon="plus" onClick={() => patch({ mounts: [...spec.mounts, { id: rid(), kind: 'volume', source: '', target: '', ro: false }] })}>{t('create.mount.add')}</Button>
      </Section>
    </>
  );
}
