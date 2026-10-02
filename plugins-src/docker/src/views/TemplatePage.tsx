import { useEffect, useMemo, useRef, useState } from 'react';
import { containers } from '../api/resources';
import { composeOf, containerPrefill, envText, slug, substitute, useTemplates, type Template } from '../api/templates';
import { deployStack, isValidStackName, readStack, writeStack } from '../api/compose';
import { COMPOSE_PROJECT } from '../api/types';
import { t } from '../i18n';
import { Button, EmptyState, Icon, Skeleton, toast } from '../kit';
import { navigate, type RouteProps } from '../router';
import { PageHeader } from '../ui/PageHeader';
import { openUrl } from '../ui/openUrl';
import { containerName } from '../api/format';
import { runText, specFromPrefill } from './create/model';
import { InstallFields, formProblems, initialForm, type FormState } from './templates/InstallForm';
import { isInstalled, Tile } from './templates/shared';

/** One app of the store (design 030 b): about, needs, install form. */
export function TemplatePage({ id }: RouteProps<'template'>) {
  const cat = useTemplates();
  const tpl = cat.templates.find((x) => x.id === id);
  if (!tpl) {
    return (
      <>
        <PageHeader icon="store" title={t('nav.templates')} back />
        {cat.loading ? <Skeleton height={240} style={{ borderRadius: 18 }} /> : <EmptyState icon="search" title={t('templates.missing.title')} text={t('templates.missing.text')} action={<Button onClick={() => navigate({ view: 'templates' }, { root: true })}>{t('templates.backToStore')}</Button>} />}
      </>
    );
  }
  return <App key={tpl.id} tpl={tpl} />;
}

function App({ tpl }: { tpl: Template }) {
  const { data: list } = containers.use();
  const defaultName = tpl.type === 'stack' ? slug(tpl.name) : slug(tpl.name);
  const [form, setForm] = useState<FormState>(() => initialForm(tpl, defaultName));
  const [showErrors, setShowErrors] = useState(false);
  const [showSpec, setShowSpec] = useState(false);
  const [specText, setSpecText] = useState('');
  const [specError, setSpecError] = useState('');
  const [busy, setBusy] = useState(false);
  const [lines, setLines] = useState<string[]>([]);
  const [failed, setFailed] = useState('');
  const logRef = useRef<HTMLPreElement>(null);

  const problems = useMemo(() => {
    const p = formProblems(tpl, form);
    const n = form.name.trim();
    if (tpl.type === 'stack') {
      if (!isValidStackName(n)) p.__name = 'templates.err.stackName';
      else if ((list ?? []).some((c) => c.Labels?.[COMPOSE_PROJECT] === n)) p.__name = 'templates.err.stackExists';
    } else if (n && (list ?? []).some((c) => containerName(c) === n)) p.__name = 'create.err.nameTaken';
    return p;
  }, [tpl, form, list]);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [lines]);

  const installed = isInstalled(tpl, list);
  const needs = tpl.needs;

  const preview = async () => {
    setSpecError('');
    if (showSpec) return setShowSpec(false);
    try {
      if (tpl.type === 'container' && tpl.container) {
        const pf = containerPrefill(tpl.container, form.values, form.name.trim());
        setSpecText(runText(specFromPrefill(tpl.container.image, pf)));
      } else {
        setSpecText(await composeOf(tpl));
      }
      setShowSpec(true);
    } catch (e) {
      setSpecError((e as Error).message);
    }
  };

  const install = async () => {
    if (Object.keys(problems).length) {
      setShowErrors(true);
      return;
    }
    const name = form.name.trim();
    if (tpl.type === 'container' && tpl.container) {
      navigate({ view: 'create', image: substitute(tpl.container.image, form.values), prefill: containerPrefill(tpl.container, form.values, name) });
      return;
    }
    setBusy(true);
    setFailed('');
    setLines([]);
    try {
      let exists = true;
      try {
        await readStack(name);
      } catch {
        exists = false;
      }
      if (exists) throw new Error(t('templates.err.stackExists'));
      setLines([t('templates.step.fetch')]);
      const compose = await composeOf(tpl);
      setLines((l) => [...l, t('templates.step.write', { name })]);
      await writeStack(name, compose, envText(form.values, tpl.variables, tpl.fixedEnv));
      const code = await deployStack(name, { pull: true }, (_s, line) => setLines((l) => (l.length > 400 ? [...l.slice(-300), line] : [...l, line])));
      if (code !== 0) throw new Error(t('templates.deploy.failed', { code }));
      toast.ok(t('templates.deployed', { name }));
      void containers.refresh();
      navigate({ view: 'stack', name }, { replace: true });
    } catch (e) {
      setFailed((e as Error).message);
      setBusy(false);
    }
  };

  const about = (
    <section className="dk-tp-panel">
      <h3>{t('templates.about')}</h3>
      <p className="dk-tp-desc">{tpl.description}</p>
      {tpl.note && <p className="dk-muted">{tpl.note}</p>}
      <dl className="dk-cr-kv">
        <dt>{t('templates.kind')}</dt>
        <dd>{tpl.type === 'stack' ? t('templates.type.stack') : t('templates.type.container')}{tpl.swarm ? `, ${t('templates.swarm')}` : ''}</dd>
        {tpl.container && <><dt>{t('create.image')}</dt><dd className="dk-mono">{tpl.container.image}</dd></>}
        <dt>{t('templates.category')}</dt>
        <dd>{tpl.category}</dd>
        <dt>{t('templates.source')}</dt>
        <dd>{tpl.sourceName}</dd>
        {tpl.website && <><dt>{t('templates.website')}</dt><dd><a className="dk-tp-link" href={tpl.website} onClick={(e) => { e.preventDefault(); openUrl(tpl.website!); }}>{tpl.website.replace(/^https?:\/\//, '')}</a></dd></>}
      </dl>
    </section>
  );

  return (
    <>
      <PageHeader icon="store" title={tpl.name} subtitle={`${tpl.category}, ${tpl.sourceName}`} back actions={tpl.website ? <Button icon="link" onClick={() => openUrl(tpl.website!)}>{t('templates.website')}</Button> : undefined} />
      <div className="dk-tp-app">
        <section className={`dk-tp-panel hue-${tpl.hue}`}>
          <div className="dk-tp-dh">
            <Tile name={tpl.name} hue={tpl.hue} size="lg" />
            <div>
              <h2>{tpl.name}</h2>
              <span className="dk-muted">{tpl.description}</span>
            </div>
          </div>
          {installed && <div className="dk-cr-hint dk-cr-hint--ok"><Icon name="check" size={14} /><span>{t('templates.alreadyInstalled')}</span></div>}
          {(needs || tpl.type) && (
            <div className="dk-tp-needs" aria-label={t('templates.needs')}>
              {needs?.ports?.map((p) => <span className="dk-tp-nd" key={String(p)}><Icon name="net" size={15} />{t('templates.need.port', { port: String(p) })}</span>)}
              {needs?.ram && <span className="dk-tp-nd"><Icon name="mem" size={15} />{needs.ram}</span>}
              {needs?.disk && <span className="dk-tp-nd"><Icon name="disk" size={15} />{needs.disk}</span>}
              <span className="dk-tp-nd"><Icon name="box" size={15} />{t((needs?.containers ?? 1) === 1 ? 'templates.need.container' : 'templates.need.containers', { n: needs?.containers ?? 1 })}</span>
              {tpl.variables.some((v) => v.type === 'password') && <span className="dk-tp-nd"><Icon name="lock" size={15} />{t('templates.need.password')}</span>}
            </div>
          )}
          {needs?.note && <div className="dk-cr-hint dk-cr-hint--warn"><Icon name="alert" size={14} /><span>{needs.note}</span></div>}
          {tpl.swarm && <div className="dk-cr-hint dk-cr-hint--warn"><Icon name="alert" size={14} /><span>{t('templates.swarm.warn')}</span></div>}
          {tpl.type === 'stack' && !tpl.compose && !tpl.composeUrl && <div className="dk-cr-hint dk-cr-hint--err" role="alert"><Icon name="alert" size={14} /><span>{t('templates.noCompose')}</span></div>}

          {busy ? (
            <div className="dk-tp-deploy" aria-live="polite">
              <h4>{t('templates.deploying', { name: form.name.trim() })}</h4>
              <pre ref={logRef} className="dk-cr-run dk-tp-log">{lines.join('\n')}</pre>
            </div>
          ) : (
            <>
              <InstallFields tpl={tpl} form={form} setForm={setForm} errors={problems} showErrors={showErrors} />
              {failed && <div className="dk-cr-fail" role="alert"><b>{t('templates.failed')}</b><p>{failed}</p></div>}
              {lines.length > 0 && failed && <pre className="dk-cr-run dk-tp-log">{lines.join('\n')}</pre>}
              {specError && <div className="dk-cr-hint dk-cr-hint--err" role="alert"><Icon name="alert" size={14} /><span>{specError}</span></div>}
              {showSpec && <pre className="dk-cr-run">{specText}</pre>}
              <div className="dk-tp-go">
                <Button icon="code" onClick={() => void preview()}>{showSpec ? t('templates.hideSpec') : tpl.type === 'stack' ? t('templates.showCompose') : t('templates.showRun')}</Button>
                <Button variant="primary" icon="download" onClick={() => void install()} disabled={tpl.type === 'stack' && !tpl.compose && !tpl.composeUrl}>{tpl.type === 'stack' ? t('templates.deploy') : t('templates.continue')}</Button>
              </div>
            </>
          )}
        </section>
        {about}
      </div>
    </>
  );
}
