import { useMemo, useState } from 'react';
import { containerName } from '../../api/format';
import { stackOf } from '../../api/model';
import { containers } from '../../api/resources';
import { t } from '../../i18n';
import { Button, Input, Segmented, Select } from '../../kit';
import type { AlertRule } from '../../settings';
import { KINDS, defaultRule, valid } from './rules';

const num = (v: string): number | undefined => (v.trim() === '' ? undefined : Number(v));

/** Inline form to add or edit one rule. */
export function RuleForm({ initial, isNew, onSave, onCancel }: { initial: AlertRule; isNew: boolean; onSave(r: AlertRule): void; onCancel(): void }) {
  const [r, setR] = useState<AlertRule>({ scope: 'all', ...initial });
  const cs = containers.use().data ?? [];
  const stacks = useMemo(() => [...new Set(cs.map(stackOf).filter(Boolean))].sort(), [cs]);
  const names = useMemo(() => cs.map(containerName).sort(), [cs]);
  const patch = (p: Partial<AlertRule>) => setR((x) => ({ ...x, ...p }));
  const scope = r.scope ?? 'all';
  const list = scope === 'stack' ? stacks : names;
  const options = [{ value: '', label: t('alerts.form.target.pick') }, ...list.map((n) => ({ value: n, label: n }))];
  if (r.target && !list.includes(r.target)) options.push({ value: r.target, label: `${r.target} (${t('alerts.form.target.missing')})` });
  const field = (label: string, value: number | undefined, key: keyof AlertRule, step = 1) => (
    <Input label={label} type="number" min={0} step={step} value={value ?? ''} onChange={(e) => patch({ [key]: num(e.target.value) } as Partial<AlertRule>)} />
  );
  const ok = valid(r);

  return (
    <div className="dk-al-form" onKeyDown={(e) => { if (e.key === 'Enter' && ok && (e.target as HTMLElement).tagName === 'INPUT') onSave(r); }}>
      <h3>{t(isNew ? 'alerts.form.new' : 'alerts.form.edit')}</h3>
      <div className="dk-al-grid">
        <Select
          label={t('alerts.form.kind')}
          hint={t(`alerts.hint.${r.kind}`)}
          value={r.kind}
          options={KINDS.map((k) => ({ value: k, label: t(`alerts.kind.${k}`) }))}
          onChange={(k) => setR({ ...defaultRule(k as AlertRule['kind']), id: r.id, enabled: r.enabled, scope: k === 'disk' ? 'all' : scope, target: k === 'disk' ? undefined : r.target })}
        />
        {r.kind !== 'disk' && (
          <div className="dk-al-scope">
            <span className="dk-al-lbl">{t('alerts.form.scope')}</span>
            <Segmented
              aria-label={t('alerts.form.scope')}
              value={scope}
              options={[
                { value: 'all', label: t('alerts.form.scope.all') },
                { value: 'stack', label: t('alerts.form.scope.stack') },
                { value: 'container', label: t('alerts.form.scope.container') },
              ]}
              onChange={(v) => patch({ scope: v as AlertRule['scope'], target: undefined })}
            />
            {scope !== 'all' && (
              <Select compact label={t(`alerts.form.target.${scope}`)} value={r.target ?? ''} options={options} onChange={(v) => patch({ target: v || undefined })} />
            )}
          </div>
        )}
      </div>
      <div className="dk-al-grid dk-al-nums">
        {r.kind === 'restart-loop' && (
          <>
            {field(t('alerts.form.count'), r.count, 'count')}
            {field(t('alerts.form.windowMin'), r.minutes, 'minutes')}
          </>
        )}
        {r.kind === 'cpu' && (
          <>
            {field(t('alerts.form.cpu'), r.threshold, 'threshold')}
            {field(t('alerts.form.forMin'), r.minutes, 'minutes')}
          </>
        )}
        {r.kind === 'memory' && (
          <>
            {field(t('alerts.form.mem'), r.threshold, 'threshold')}
            {field(t('alerts.form.forMin'), r.minutes, 'minutes')}
            <div className="dk-al-scope">
              <span className="dk-al-lbl">{t('alerts.form.memBasis')}</span>
              <Segmented
                aria-label={t('alerts.form.memBasis')}
                value={r.memBasis ?? 'limit'}
                options={[
                  { value: 'limit', label: t('alerts.form.memBasis.limit') },
                  { value: 'host', label: t('alerts.form.memBasis.host') },
                ]}
                onChange={(v) => patch({ memBasis: v as 'limit' | 'host' })}
              />
            </div>
          </>
        )}
        {r.kind === 'disk' && field(t('alerts.form.disk'), r.threshold, 'threshold')}
      </div>
      {!ok && <p className="dk-al-warn">{t('alerts.form.invalid')}</p>}
      <div className="dk-al-btns">
        <Button variant="primary" icon="check" disabled={!ok} onClick={() => onSave(r)}>{t('alerts.form.save')}</Button>
        <Button variant="ghost" onClick={onCancel}>{t('common.cancel')}</Button>
      </div>
    </div>
  );
}
