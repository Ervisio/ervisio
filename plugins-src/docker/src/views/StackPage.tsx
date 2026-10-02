import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { deleteStack, deployStack, isValidStackName, moveToManaged, projectConfig, readStack, serviceNamesOf, servicesOf, stackAction, stackDir, writeStack, COMPOSE_FILE, DEPLOYED_FILE, STACKS_DIR, type Stack, type StackAction, type StackFiles } from '../api/compose';
import { t } from '../i18n';
import { Badge, Button, Card, Checkbox, DropdownMenu, EmptyState, Icon, Input, Skeleton, toast } from '../kit';
import { back, navigate, type RouteProps } from '../router';
import { getSdk } from '../sdk';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { CodeEditor, jumpToLine } from './stack/CodeEditor';
import { Confirm } from './stack/Confirm';
import { DeployOutput } from './stack/DeployCard';
import { useDeploy, logged } from './stack/deployLog';
import { DiffView } from './stack/DiffView';
import { diffLines, summarize } from './stack/diff';
import { ServicesCard } from './stack/ServicesCard';
import { SetupCard } from './stack/SetupCard';
import { useStacks } from './stack/useStacks';
import { validateCompose, validateEnv, type Issue, type Validation } from './stack/validate';

type Tab = 'compose' | 'env' | 'diff';

const STARTER = `services:
  app:
    image: nginx:alpine
    ports:
      - "8080:80"
    restart: unless-stopped
`;

/** Route { view: 'stack', name }. An empty name means a new stack. */
export function StackPage({ name }: RouteProps<'stack'>) {
  if (name === '') return <NewStack />;
  return <ExistingStack name={name} />;
}

function ExistingStack({ name }: { name: string }) {
  const { stacks, sources, loading, error, reload } = useStacks();
  const stack = stacks.find((s) => s.name === name);
  if (!stack && error) return <><PageHeader icon="layers" title={name} back /><ErrorState error={error} onRetry={() => void reload()} /></>;
  if (!stack && loading) return <><PageHeader icon="layers" title={name} back /><Skeleton height={320} style={{ borderRadius: 18 }} /></>;
  if (!stack) {
    return (
      <>
        <PageHeader icon="layers" title={name} back />
        <EmptyState icon="layers" hue="file" title={t('stacks.missing.title', { name })} text={t('stacks.missing.text')} action={<Button onClick={() => navigate({ view: 'stacks' }, { root: true })}>{t('stacks.title')}</Button>} />
      </>
    );
  }
  void sources;
  return stack.managed ? <ManagedStack stack={stack} reload={reload} /> : <DetectedStack stack={stack} reload={reload} />;
}

/* ---------- shared pieces ---------- */

/** The strip under the editor tabs: valid, or the problems with clickable line numbers. */
function Validity({ v, envIssues, changed, deployed, dirty, onJump }: {
  v: Validation;
  envIssues: Issue[];
  changed: number;
  deployed: boolean;
  dirty: boolean;
  onJump(tab: Tab, line: number): void;
}) {
  const errs = v.issues.filter((i) => i.level === 'error');
  const warns = v.issues.filter((i) => i.level === 'warn');
  const list = [...errs.map((i) => ({ i, tab: 'compose' as Tab })), ...envIssues.filter((i) => i.level === 'error').map((i) => ({ i, tab: 'env' as Tab })), ...warns.map((i) => ({ i, tab: 'compose' as Tab }))];
  const tone = errs.length || envIssues.some((i) => i.level === 'error') ? 'err' : warns.length ? 'warn' : 'ok';
  const summary =
    tone === 'ok'
      ? t('stacks.valid.ok') + ' ' + (deployed ? (changed ? t('stacks.valid.changes', { n: changed }) : t('stacks.valid.noChanges')) : t('stacks.valid.neverDeployed'))
      : tone === 'err'
        ? t('stacks.valid.errors', { n: errs.length + envIssues.filter((i) => i.level === 'error').length })
        : t('stacks.valid.warnings', { n: warns.length });
  return (
    <div className={`dk-sk-valid dk-sk-valid--${tone}`} role="status">
      <div className="dk-sk-valid-h">
        <Icon name={tone === 'ok' ? 'check' : 'alert'} />
        <span>{summary}</span>
        {dirty && <span className="dk-sk-unsaved">{t('stacks.unsaved')}</span>}
      </div>
      {list.length > 0 && (
        <ul>
          {list.slice(0, 6).map(({ i, tab }, k) => (
            <li key={k}>
              <button type="button" onClick={() => onJump(tab, i.line)}>{tab === 'env' ? '.env ' : ''}{t('stacks.line', { n: i.line })}</button>
              <span>{t(i.key, i.vars)}</span>
            </li>
          ))}
          {list.length > 6 && <li><span>{t('stacks.moreIssues', { n: list.length - 6 })}</span></li>}
        </ul>
      )}
    </div>
  );
}

function Tabs3({ tab, onTab, diffCount, canDiff }: { tab: Tab; onTab(t: Tab): void; diffCount: number; canDiff: boolean }) {
  const item = (id: Tab, label: string, extra?: React.ReactNode) => (
    <button type="button" key={id} className={tab === id ? 'on' : ''} role="tab" aria-selected={tab === id} onClick={() => onTab(id)}>{label}{extra}</button>
  );
  return (
    <div className="dk-sk-tabs" role="tablist">
      {item('compose', 'compose.yaml')}
      {item('env', '.env')}
      {canDiff && item('diff', t('stacks.tab.diff'), diffCount > 0 ? <i className="dk-sk-cnt">{diffCount}</i> : null)}
    </div>
  );
}

const useAlive = () => {
  const r = useRef(true);
  useEffect(() => () => { r.current = false; }, []);
  return r;
};

/* ---------- managed stack ---------- */

function ManagedStack({ stack, reload }: { stack: Stack; reload(): Promise<void> }) {
  const { name } = stack;
  const [files, setFiles] = useState<StackFiles | null>(null);
  const [loadErr, setLoadErr] = useState<Error | null>(null);
  const [compose, setCompose] = useState('');
  const [env, setEnv] = useState('');
  const [tab, setTab] = useState<Tab>('compose');
  const [pull, setPull] = useState(true);
  const [deployedAt, setDeployedAt] = useState<number>();
  const [confirm, setConfirm] = useState<'down' | 'delete' | null>(null);
  const [rmVolumes, setRmVolumes] = useState(false);
  const [rmFolder, setRmFolder] = useState(true);
  const [saving, setSaving] = useState(false);
  const alive = useAlive();
  const edRef = useRef<HTMLDivElement>(null);
  const out = useDeploy(name);
  const running = !!out?.running;

  const load = useCallback(async () => {
    try {
      const f = await readStack(name);
      if (!alive.current) return;
      setFiles(f);
      setLoadErr(null);
      return f;
    } catch (e) {
      if (alive.current) setLoadErr(e as Error);
    }
  }, [name, alive]);

  const loadMtime = useCallback(async () => {
    try {
      const list = await getSdk().files.list(stackDir(name));
      const d = list.find((e) => e.name === DEPLOYED_FILE);
      if (alive.current) setDeployedAt(d?.mtime);
    } catch {
      /* no folder access: no time */
    }
  }, [name, alive]);

  useEffect(() => {
    void load().then((f) => {
      if (f) {
        setCompose(f.compose);
        setEnv(f.env);
      }
    });
    void loadMtime();
  }, [load, loadMtime]);

  const v = useMemo(() => validateCompose(compose, env), [compose, env]);
  const envIssues = useMemo(() => validateEnv(env), [env]);
  const diff = useMemo(() => (files?.deployed != null ? diffLines(files.deployed, compose) : null), [files, compose]);
  const sum = useMemo(() => (diff ? summarize(diff) : null), [diff]);
  const changedCount = sum ? sum.added + sum.removed : 0;
  const dirty = !!files && (compose !== files.compose || env !== files.env);
  const blocked = v.errors > 0 || envIssues.some((i) => i.level === 'error');

  const svcNames = useMemo(() => v.services.map((s) => s.name), [v.services]);
  const images = useMemo(() => Object.fromEntries(v.services.map((s) => [s.name, s.image])), [v.services]);
  const services = useMemo(() => servicesOf(stack.containers, svcNames.length ? svcNames : serviceNamesOf(compose)), [stack.containers, svcNames, compose]);
  const upCount = stack.running;

  const jump = (tb: Tab, line: number) => {
    setTab(tb);
    setTimeout(() => jumpToLine(edRef.current, line), 30);
  };

  const save = async (): Promise<boolean> => {
    setSaving(true);
    try {
      await writeStack(name, compose, env);
      setFiles((f) => (f ? { ...f, compose, env } : f));
      return true;
    } catch (e) {
      toast.err(t('stacks.saveFail'), (e as Error).message);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const afterRun = async (code: number, okMsg: string) => {
    if (code === 0) toast.ok(okMsg);
    else toast.err(t('stacks.runFail'), t('stacks.runFailText'));
    await Promise.all([load(), loadMtime(), reload()]);
  };

  const deploy = async () => {
    if (dirty && !(await save())) return;
    const code = await logged(name, t('stacks.deploy.title'), (on) => deployStack(name, { pull }, on));
    await afterRun(code, t('stacks.deploy.done', { name }));
  };

  const run = async (action: StackAction) => {
    if (dirty && action === 'pull' && !(await save())) return;
    const code = await logged(name, t(`stacks.act.${action}`), (on) => {
      on('stdout', `$ docker compose ${action}`);
      return stackAction(name, action, on);
    });
    await afterRun(code, t(`stacks.done.${action}`, { name }));
  };

  const doDelete = async () => {
    const code = await logged(name, t('stacks.delete.title', { name }), (on) => deleteStack(name, { volumes: rmVolumes, folder: rmFolder }, on));
    if (code === 0) {
      toast.ok(t('stacks.delete.done', { name }));
      await reload();
      navigate({ view: 'stacks' }, { root: true });
    } else {
      toast.err(t('stacks.runFail'), t('stacks.runFailText'));
      await reload();
    }
  };

  const total = Math.max(stack.total, 0);
  const badge = total ? (
    <Badge tone={upCount === total ? 'ok' : upCount ? 'warn' : 'neutral'} dot>{t('stacks.runningOf', { running: upCount, total })}</Badge>
  ) : (
    <Badge tone="neutral">{t('stacks.notDeployed')}</Badge>
  );

  const header = (
    <PageHeader
      icon="layers"
      title={<>{name} {badge}</>}
      subtitle={<span className="dk-sk-path">{stackDir(name)}/{COMPOSE_FILE}</span>}
      back
      actions={
        <>
          <Button icon="download" disabled={running} onClick={() => run('pull')}>{t('stacks.pull')}</Button>
          <Button icon="stop" disabled={running || !total} onClick={() => setConfirm('down')}>{t('stacks.down')}</Button>
          <Button icon="refresh" disabled={running || !total} onClick={() => run('restart')}>{t('common.restart')}</Button>
          <Button variant="primary" icon="play" loading={running && out?.title === t('stacks.deploy.title')} disabled={running || blocked || !files} title={blocked ? t('stacks.fixFirst') : undefined} onClick={deploy}>{t('stacks.deployChanges')}</Button>
          <DropdownMenu
            aria-label={t('stacks.more')}
            items={[{ id: 'delete', label: t('stacks.delete.menu'), icon: 'trash', danger: true, onSelect: () => setConfirm('delete') }]}
            trigger={(p) => <Button variant="ghost" icon="more" iconOnly aria-label={t('stacks.more')} {...p} />}
          />
        </>
      }
    />
  );

  if (loadErr && !files) {
    return (
      <>
        {header}
        <EmptyState icon="alert" hue="file" title={t('stacks.readFail')} text={loadErr.message} action={<Button onClick={() => void load().then((f) => f && (setCompose(f.compose), setEnv(f.env)))}>{t('common.retry')}</Button>} />
      </>
    );
  }
  if (!files) return <>{header}<Skeleton height={320} style={{ borderRadius: 18 }} /></>;

  return (
    <>
      {header}
      <div className="dk-sk-grid">
        <Card className="dk-sk-edcard">
          <div className="dk-sk-edh">
            <Tabs3 tab={tab} onTab={setTab} diffCount={changedCount} canDiff={files.deployed != null} />
            <Button size="sm" variant="ghost" icon="download" disabled={!dirty || saving} loading={saving} onClick={() => void save().then((ok) => ok && toast.ok(t('stacks.saved')))}>{t('common.save')}</Button>
          </div>
          <Validity v={v} envIssues={envIssues} changed={changedCount} deployed={files.deployed != null} dirty={dirty} onJump={jump} />
          <div ref={edRef}>
            {tab === 'compose' && <CodeEditor value={compose} onChange={setCompose} lang="yaml" issues={v.issues} changed={sum?.changedLines} label="compose.yaml" />}
            {tab === 'env' && <CodeEditor value={env} onChange={setEnv} lang="env" issues={envIssues} label=".env" />}
            {tab === 'diff' && files.deployed != null && <DiffView oldText={files.deployed} newText={compose} />}
          </div>
          {tab === 'env' && <p className="dk-muted">{t('stacks.envHint')}</p>}
        </Card>
        <div className="dk-sk-col">
          <Card title={t('stacks.services')}>
            <ServicesCard services={services} images={images} />
          </Card>
          <Card
            title={t('stacks.lastDeploy')}
            action={deployedAt ? <span className="dk-muted">{t('stacks.deploy.lastAt', { ago: new Date(deployedAt > 1e11 ? deployedAt : deployedAt * 1000).toLocaleString() })}</span> : undefined}
          >
            <Checkbox checked={pull} onChange={setPull} label={t('stacks.pullFirst')} />
            <DeployOutput state={out} deployedAt={deployedAt} />
          </Card>
        </div>
      </div>

      <Confirm
        open={confirm === 'down'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void run('down'); }}
        title={t('stacks.downTitle', { name })}
        description={t('stacks.downText')}
        confirmLabel={t('stacks.down')}
        icon="stop"
      />
      <Confirm
        open={confirm === 'delete'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void doDelete(); }}
        title={t('stacks.delete.title', { name })}
        description={t('stacks.delete.text', { dir: stackDir(name) })}
        confirmLabel={t('stacks.delete.confirm')}
        confirmText={name}
        danger
        icon="trash"
      >
        <Checkbox checked={rmVolumes} onChange={setRmVolumes} label={t('stacks.delete.volumes')} />
        <Checkbox checked={rmFolder} onChange={setRmFolder} label={t('stacks.delete.folder', { dir: stackDir(name) })} />
      </Confirm>
    </>
  );
}

/* ---------- detected stack (started somewhere else) ---------- */

function DetectedStack({ stack, reload }: { stack: Stack; reload(): Promise<void> }) {
  const { name } = stack;
  const file = stack.configFiles[0];
  const [cfg, setCfg] = useState<{ ok: boolean; text: string } | null>(null);
  const [moving, setMoving] = useState(false);
  const [confirm, setConfirm] = useState<'down' | 'move' | null>(null);
  const alive = useAlive();
  const out = useDeploy(name);
  const running = !!out?.running || moving;

  useEffect(() => {
    if (!file) { setCfg({ ok: false, text: t('stacks.detected.noFile') }); return; }
    setCfg(null);
    void projectConfig(name, file).then((c) => alive.current && setCfg(c)).catch((e) => alive.current && setCfg({ ok: false, text: (e as Error).message }));
  }, [name, file, alive]);

  const services = useMemo(() => servicesOf(stack.containers), [stack.containers]);
  const images = useMemo(() => ({}), []);

  const run = async (action: StackAction) => {
    const code = await logged(name, t(`stacks.act.${action}`), (on) => {
      on('stdout', `$ docker compose --project-name ${name} ${action}`);
      return stackAction(name, action, on, { detected: true });
    });
    if (code === 0) toast.ok(t(`stacks.done.${action}`, { name }));
    else toast.err(t('stacks.runFail'), t('stacks.runFailText'));
    await reload();
  };

  const move = async () => {
    if (!file) return;
    setMoving(true);
    try {
      await moveToManaged(name, file);
      toast.ok(t('stacks.move.done', { name }));
      await reload();
    } catch (e) {
      toast.err(t('stacks.move.fail'), (e as Error).message);
    } finally {
      if (alive.current) setMoving(false);
    }
  };

  const total = stack.total;
  const up = stack.running;
  return (
    <>
      <PageHeader
        icon="layers"
        title={<>{name} <Badge tone="info">{t('stacks.detected')}</Badge></>}
        subtitle={<span className="dk-sk-path">{file || stack.dir}</span>}
        back
        actions={
          <>
            {up < total && <Button icon="play" disabled={running} onClick={() => run('start')}>{t('common.start')}</Button>}
            {up > 0 && <Button icon="stop" disabled={running} onClick={() => run('stop')}>{t('common.stop')}</Button>}
            <Button icon="refresh" disabled={running || !total} onClick={() => run('restart')}>{t('common.restart')}</Button>
            <Button icon="trash" disabled={running || !total} onClick={() => setConfirm('down')}>{t('stacks.down')}</Button>
            <Button variant="primary" icon="download" loading={moving} disabled={running || !cfg?.ok} onClick={() => setConfirm('move')}>{t('stacks.move.button')}</Button>
          </>
        }
      />
      <div className="dk-sk-note"><Icon name="info" /><div><b>{t('stacks.detected.title')}</b><p>{t('stacks.detected.text', { dir: stack.dir || '?' })}</p></div></div>
      <div className="dk-sk-grid">
        <Card className="dk-sk-edcard" title={t('stacks.detected.config')}>
          {!cfg ? <Skeleton height={200} style={{ borderRadius: 14 }} /> : cfg.ok ? <CodeEditor value={cfg.text} lang="yaml" readOnly label={t('stacks.detected.config')} /> : <p className="dk-sk-err">{cfg.text}</p>}
          {stack.configFiles.length > 1 && <p className="dk-muted">{t('stacks.detected.multi', { file })}</p>}
        </Card>
        <div className="dk-sk-col">
          <Card title={t('stacks.services')}><ServicesCard services={services} images={images} /></Card>
          <Card title={t('stacks.lastAction')}><DeployOutput state={out} /></Card>
        </div>
      </div>
      <Confirm
        open={confirm === 'down'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void run('down'); }}
        title={t('stacks.downTitle', { name })}
        description={t('stacks.downText')}
        confirmLabel={t('stacks.down')}
        icon="stop"
      />
      <Confirm
        open={confirm === 'move'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void move(); }}
        title={t('stacks.move.title', { name })}
        description={t('stacks.move.text', { dir: stackDir(name), orig: stack.dir || file || '?' })}
        confirmLabel={t('stacks.move.confirm')}
        danger={false}
        icon="download"
      />
    </>
  );
}

/* ---------- new stack ---------- */

function NewStack() {
  const { stacks, sources, reload } = useStacks();
  const [name, setName] = useState('');
  const [compose, setCompose] = useState(STARTER);
  const [env, setEnv] = useState('');
  const [tab, setTab] = useState<Tab>('compose');
  const [pull, setPull] = useState(true);
  const [busy, setBusy] = useState(false);
  const edRef = useRef<HTMLDivElement>(null);

  const v = useMemo(() => validateCompose(compose, env), [compose, env]);
  const envIssues = useMemo(() => validateEnv(env), [env]);
  const exists = stacks.some((s) => s.name === name);
  const nameErr = name && !isValidStackName(name) ? t('stacks.new.nameBad') : exists ? t('stacks.new.nameTaken') : undefined;
  const ready = !!name && !nameErr && v.errors === 0 && !envIssues.some((i) => i.level === 'error');

  const create = async (andDeploy: boolean) => {
    setBusy(true);
    try {
      await writeStack(name, compose, env);
    } catch (e) {
      setBusy(false);
      toast.err(t('stacks.saveFail'), (e as Error).message);
      return;
    }
    setBusy(false);
    toast.ok(t('stacks.new.created', { name }));
    await reload();
    navigate({ view: 'stack', name }, { replace: true });
    if (andDeploy) {
      const code = await logged(name, t('stacks.deploy.title'), (on) => deployStack(name, { pull }, on));
      if (code === 0) toast.ok(t('stacks.deploy.done', { name }));
      else toast.err(t('stacks.runFail'), t('stacks.runFailText'));
      await reload();
    }
  };

  const header = <PageHeader icon="layers" title={t('stacks.new.title')} subtitle={t('stacks.new.sub', { dir: STACKS_DIR })} back />;
  if (sources && !sources.folder) return <>{header}<SetupCard onDone={() => void reload()} /></>;

  return (
    <>
      {header}
      <div className="dk-sk-new">
        <Card className="dk-sk-edcard">
          <div className="dk-sk-newname">
            <Input label={t('stacks.new.name')} value={name} mono placeholder="my-app" error={nameErr} hint={nameErr ? undefined : t('stacks.new.nameHint', { dir: `${STACKS_DIR}/${name || '<name>'}` })} onChange={(e) => setName(e.target.value.toLowerCase())} maxLength={63} autoFocus />
          </div>
          <div className="dk-sk-edh">
            <Tabs3 tab={tab} onTab={setTab} diffCount={0} canDiff={false} />
            <span className="dk-muted">{t('stacks.new.paste')}</span>
          </div>
          <Validity v={v} envIssues={envIssues} changed={0} deployed={false} dirty={false} onJump={(tb, l) => { setTab(tb); setTimeout(() => jumpToLine(edRef.current, l), 30); }} />
          <div ref={edRef}>
            {tab === 'compose' ? <CodeEditor value={compose} onChange={setCompose} lang="yaml" issues={v.issues} label="compose.yaml" minLines={18} /> : <CodeEditor value={env} onChange={setEnv} lang="env" issues={envIssues} label=".env" minLines={18} />}
          </div>
          {tab === 'env' && <p className="dk-muted">{t('stacks.envHint')}</p>}
        </Card>
        <div className="dk-sk-col">
          <Card title={t('stacks.new.create')}>
            <Checkbox checked={pull} onChange={setPull} label={t('stacks.pullFirst')} />
            <Button variant="primary" icon="play" block loading={busy} disabled={!ready || busy} onClick={() => void create(true)}>{t('stacks.new.createDeploy')}</Button>
            <Button block icon="download" disabled={!ready || busy} onClick={() => void create(false)}>{t('stacks.new.createOnly')}</Button>
            <Button block variant="ghost" onClick={back}>{t('common.cancel')}</Button>
          </Card>
        </div>
      </div>
    </>
  );
}
