import { t } from '../../i18n';
import { Switch } from '../../kit';
import { Hint, Section, type StepProps } from './parts';
import { runText, NO_PORTS_MODES, type Spec } from './model';

export function ReviewStep({ spec, patch, recreate }: StepProps & { recreate: boolean }) {
  return (
    <>
      <Section title={t('create.review.run')} note={t('create.review.runNote')}>
        <pre className="dk-cr-run">{runText(spec)}</pre>
      </Section>
      <Section title={t('create.review.options')}>
        <Switch checked={spec.start} onChange={(v) => patch({ start: v })} label={t('create.review.start')} />
        {recreate && <Hint icon="info">{t('create.review.recreate')}</Hint>}
        {spec.privileged && <Hint tone="warn" icon="alert">{t('create.privileged.warn')}</Hint>}
      </Section>
    </>
  );
}

/** The card on the right: what the container will be so far. */
export function Summary({ spec, title }: { spec: Spec; title: string }) {
  const ports = NO_PORTS_MODES(spec.network) ? [] : spec.ports.filter((p) => p.container.trim());
  const folders = spec.mounts.filter((m) => m.kind === 'bind' && m.source.trim()).length;
  const vols = spec.mounts.filter((m) => m.kind === 'volume' && m.source.trim()).length;
  const limits = [spec.memoryMb.trim() && `${spec.memoryMb.trim()} MB`, spec.cpus.trim() && `${spec.cpus.trim()} CPU`].filter(Boolean).join(', ');
  const storage = [folders && t(folders === 1 ? 'create.sum.folder' : 'create.sum.folders', { n: folders }), vols && t(vols === 1 ? 'create.sum.volume' : 'create.sum.volumes', { n: vols })].filter(Boolean).join(', ');
  const row = (k: string, v: string | undefined, mono?: boolean) =>
    v ? (
      <>
        <dt>{k}</dt>
        <dd className={mono ? 'dk-mono' : undefined}>{v}</dd>
      </>
    ) : null;
  return (
    <section className="dk-cr-card">
      <h3>{title}</h3>
      <dl className="dk-cr-kv">
        {row(t('create.image'), spec.image.trim() || '–', true)}
        {row(t('create.name'), spec.name.trim() || t('create.sum.autoName'))}
        {row(t('create.restart'), t(`create.restart.${spec.restart}`))}
        {row(t('create.ports'), ports.map((p) => `${p.host.trim() || t('create.port.random')} → ${p.container.trim()}${p.proto === 'udp' ? '/udp' : ''}`).join(', '), true)}
        {row(t('create.storage'), storage)}
        {row(t('create.env'), spec.env.filter((e) => e.key.trim()).length ? t(spec.env.filter((e) => e.key.trim()).length === 1 ? 'create.sum.var' : 'create.sum.vars', { n: spec.env.filter((e) => e.key.trim()).length }) : '')}
        {row(t('create.network'), spec.network === 'bridge' ? t('create.net.bridge') : spec.network)}
        {row(t('create.limits'), limits)}
        {row(t('create.adv.privileged'), spec.privileged ? t('common.yes') : '')}
      </dl>
    </section>
  );
}
