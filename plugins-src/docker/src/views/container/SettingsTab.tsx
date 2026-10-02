import { useState } from 'react';
import { docker, errorText } from '../../api/engine';
import { containers } from '../../api/resources';
import type { ContainerInspect } from '../../api/types';
import { t } from '../../i18n';
import { Button, Field, Input, Select, toast } from '../../kit';
import { navigate } from '../../router';
import { nameOf } from './util';

const GB = 1024 ** 3;
const MB = 1024 ** 2;

export function SettingsTab({ inspect, onChanged }: { inspect: ContainerInspect; onChanged(): void }) {
  return (
    <div className="dk-c-ov">
      <RenameCard inspect={inspect} onChanged={onChanged} />
      <LimitsCard inspect={inspect} onChanged={onChanged} />
      <div className="dk-card">
        <h3>{t('container.set.recreate')}</h3>
        <p className="dk-muted dk-c-p">{t('container.set.recreateNote')}</p>
        <div><Button variant="primary" icon="edit" onClick={() => navigate({ view: 'create', from: inspect.Id })}>{t('container.edit')}</Button></div>
      </div>
    </div>
  );
}

function RenameCard({ inspect, onChanged }: { inspect: ContainerInspect; onChanged(): void }) {
  const current = nameOf(inspect.Name);
  const [name, setName] = useState(current);
  const [busy, setBusy] = useState(false);
  const valid = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(name);
  const save = async () => {
    setBusy(true);
    try {
      await docker.post(`/containers/${encodeURIComponent(inspect.Id)}/rename`, { name });
      toast.ok(t('container.set.renamed', { name }));
      void containers.refresh();
      onChanged();
    } catch (e) {
      toast.err(t('container.set.renameFail'), errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    // No <form>: the plugin frame is sandboxed without allow-forms, so a submit would be blocked.
    <div className="dk-card" onKeyDown={(e) => { if (e.key === 'Enter' && valid && name !== current && !busy) void save(); }}>
      <h3>{t('container.set.rename')}</h3>
      <div className="dk-c-form-row">
        <Input fieldClassName="dk-c-grow" mono value={name} onChange={(e) => setName(e.target.value)} aria-label={t('container.set.rename')} error={name && !valid ? t('container.set.nameBad') : undefined} />
        <Button variant="primary" loading={busy} disabled={!valid || name === current} onClick={() => void save()}>{t('common.save')}</Button>
      </div>
      {inspect.Config.Labels?.['com.docker.compose.project'] && <p className="dk-muted dk-c-p">{t('container.set.renameCompose')}</p>}
    </div>
  );
}

const POLICIES = ['no', 'always', 'unless-stopped', 'on-failure'];

function LimitsCard({ inspect, onChanged }: { inspect: ContainerInspect; onChanged(): void }) {
  const hc = inspect.HostConfig;
  const mem = hc.Memory ?? 0;
  const [policy, setPolicy] = useState(hc.RestartPolicy?.Name || 'no');
  const [retries, setRetries] = useState(String(hc.RestartPolicy?.MaximumRetryCount ?? 0));
  const [memVal, setMemVal] = useState(mem > 0 ? String(mem >= GB && mem % GB === 0 ? mem / GB : Math.round(mem / MB)) : '');
  const [memUnit, setMemUnit] = useState(mem >= GB && mem % GB === 0 ? 'GB' : 'MB');
  const [cpus, setCpus] = useState(hc.NanoCpus ? String(hc.NanoCpus / 1e9) : '');
  const [busy, setBusy] = useState(false);

  const memNum = memVal.trim() === '' ? 0 : Number(memVal);
  const cpuNum = cpus.trim() === '' ? 0 : Number(cpus);
  const memBytes = Math.round(memNum * (memUnit === 'GB' ? GB : MB));
  // The Engine reads 0 as "leave as is", so a limit that is set cannot be removed in place, only changed.
  const memGone = mem > 0 && memNum === 0;
  const cpuGone = !!hc.NanoCpus && cpuNum === 0;
  const memBad = !isFinite(memNum) || memNum < 0 || (memNum > 0 && memBytes < 6 * MB) || memGone;
  const cpuBad = !isFinite(cpuNum) || cpuNum < 0 || cpuGone;
  const retryNum = Math.max(0, Math.floor(Number(retries) || 0));

  const apply = async () => {
    setBusy(true);
    try {
      const body: Record<string, unknown> = {
        RestartPolicy: { Name: policy === 'no' ? '' : policy, MaximumRetryCount: policy === 'on-failure' ? retryNum : 0 },
      };
      if (memBytes > 0 && memBytes !== mem) {
        body.Memory = memBytes;
        // Keep swap unlimited when it was; otherwise the usual twice the memory, so swap never ends up below it.
        body.MemorySwap = hc.MemorySwap === -1 ? -1 : memBytes * 2;
      }
      if (cpuNum > 0 && Math.round(cpuNum * 1e9) !== hc.NanoCpus) body.NanoCpus = Math.round(cpuNum * 1e9);
      await docker.post(`/containers/${encodeURIComponent(inspect.Id)}/update`, undefined, body);
      toast.ok(t('container.set.updated'));
      void containers.refresh();
      onChanged();
    } catch (e) {
      toast.err(t('container.set.updateFail'), errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="dk-card" onKeyDown={(e) => { if (e.key === 'Enter' && !memBad && !cpuBad && !busy && (e.target as HTMLElement).tagName === 'INPUT') void apply(); }}>
      <h3>{t('container.set.limits')}</h3>
      <div className="dk-c-form-grid">
        <Select label={t('container.set.policy')} options={POLICIES.map((p) => ({ value: p, label: t(`container.set.policy.${p}`) }))} value={policy} onChange={setPolicy} />
        {policy === 'on-failure' && <Input label={t('container.set.retries')} type="number" min={0} value={retries} onChange={(e) => setRetries(e.target.value)} hint={t('container.set.retriesHint')} />}
        <Field label={t('container.set.memory')} hint={memBad ? undefined : t('container.set.memoryHint')} error={memGone ? t('container.set.noRemove') : memBad ? t('container.set.memoryBad') : undefined}>
          <div className="dk-c-form-row">
            <Input fieldClassName="dk-c-grow" type="number" min={0} step="any" placeholder={t('container.set.unlimited')} value={memVal} onChange={(e) => setMemVal(e.target.value)} aria-label={t('container.set.memory')} />
            <span className="dk-c-sel dk-c-sel--xs"><Select compact options={[{ value: 'MB', label: 'MB' }, { value: 'GB', label: 'GB' }]} value={memUnit} onChange={setMemUnit} /></span>
          </div>
        </Field>
        <Input label={t('container.set.cpus')} type="number" min={0} step="any" placeholder={t('container.set.unlimited')} value={cpus} onChange={(e) => setCpus(e.target.value)} error={cpuGone ? t('container.set.noRemove') : cpuBad ? t('container.set.cpusBad') : undefined} hint={t('container.set.cpusHint')} />
      </div>
      <div><Button variant="primary" loading={busy} disabled={memBad || cpuBad} onClick={() => void apply()}>{t('container.set.apply')}</Button></div>
      <p className="dk-muted dk-c-p">{t('container.set.limitsNote')}</p>
    </div>
  );
}
