import { useEffect, useMemo, useRef, useState } from 'react';
import { classify, docker } from '../api/engine';
import { containerName } from '../api/format';
import { useAsync } from '../api/hooks';
import { containers, info } from '../api/resources';
import type { ContainerInspect } from '../api/types';
import { t } from '../i18n';
import { Button, Icon, Skeleton, toast } from '../kit';
import { navigate, type RouteProps } from '../router';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { EnvStep } from './create/EnvStep';
import { ImageStep } from './create/ImageStep';
import { NetworkStep } from './create/NetworkStep';
import { ReviewStep, Summary } from './create/ReviewStep';
import { RunView } from './create/RunView';
import { StorageStep } from './create/StorageStep';
import { problems as checkProblems, specFromInspect, specFromPrefill, usedHostPorts, type Base, type Spec } from './create/model';
import type { PullState } from './create/pull';
import { runCreate, type RunControl, type RunResult, type Step } from './create/run';

const STEPS = ['image', 'storage', 'env', 'network', 'review'] as const;
/** Which problem keys block which step. */
const BLOCKS: Record<number, (keyof ReturnType<typeof checkProblems>)[]> = {
  0: ['image', 'name'],
  1: ['ports', 'mounts'],
  2: ['env'],
  3: ['limits'],
};

/** New container wizard (design 028 b). With `from` it edits an existing container by recreating it. */
export function CreatePage({ from, image, prefill }: RouteProps<'create'>) {
  const loaded = useAsync(
    async (): Promise<{ spec: Spec; base?: Base } | undefined> => {
      if (!from) return { spec: specFromPrefill(image, prefill) };
      const ins = await docker.get<ContainerInspect>(`/containers/${encodeURIComponent(from)}/json`);
      const imgRef = ins.Image;
      let cfg: Base['imageConfig'];
      try {
        cfg = (await docker.get<{ Config?: Base['imageConfig'] }>(`/images/${encodeURIComponent(imgRef)}/json`)).Config;
      } catch {
        cfg = undefined;
      }
      return specFromInspect(ins, cfg);
    },
    [from],
  );

  const header = (title: string, sub?: string, actions?: React.ReactNode) => <PageHeader icon="plus" title={title} subtitle={sub} back actions={actions} />;

  if (loaded.error && !loaded.data) {
    return (
      <>
        {header(t('create.title'))}
        <ErrorState error={classify(loaded.error)} onRetry={loaded.reload} />
      </>
    );
  }
  if (!loaded.data) {
    return (
      <>
        {header(from ? t('create.edit') : t('create.title'))}
        <Skeleton height={160} style={{ borderRadius: 18 }} />
      </>
    );
  }
  return <Wizard key={`${from ?? ''}|${image ?? ''}`} from={from} initial={loaded.data.spec} base={loaded.data.base} />;
}

function Wizard({ from, initial, base }: { from?: string; initial: Spec; base?: Base }) {
  const recreate = !!base;
  const [spec, setSpec] = useState<Spec>(initial);
  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(0);
  const [showErrors, setShowErrors] = useState(false);
  const [running, setRunning] = useState(false);
  const [steps, setSteps] = useState<Step[]>([]);
  const [pull, setPull] = useState<PullState | undefined>();
  const [result, setResult] = useState<RunResult | undefined>();
  const control = useRef<RunControl | undefined>();
  const { data: list } = containers.use();
  const ncpu = info.use().data?.info.NCPU;

  const patch = (p: Partial<Spec>) => setSpec((s) => ({ ...s, ...p }));
  useEffect(() => () => control.current?.cancel(), []);

  const names = useMemo(() => new Set((list ?? []).filter((c) => c.Id !== from).map(containerName)), [list, from]);
  const used = useMemo(() => usedHostPorts(list, from), [list, from]);
  const probs = checkProblems(spec, { names, used, recreate });

  const blocked = (s: number) => (BLOCKS[s] ?? []).some((k) => probs[k]);
  const go = (to: number) => {
    setShowErrors(false);
    setStep(to);
    setReached((r) => Math.max(r, to));
  };
  const next = () => {
    if (blocked(step)) {
      setShowErrors(true);
      return;
    }
    go(Math.min(STEPS.length - 1, step + 1));
  };
  const jump = (to: number) => {
    // Going back is always allowed; going forward only over steps that are valid.
    if (to <= step) return go(to);
    for (let s = step; s < to; s++) {
      if (blocked(s)) {
        setStep(s);
        setShowErrors(true);
        return;
      }
    }
    go(to);
  };

  const submit = () => {
    for (let s = 0; s < STEPS.length - 1; s++) {
      if (blocked(s)) {
        setStep(s);
        setShowErrors(true);
        return;
      }
    }
    setRunning(true);
    setResult(undefined);
    setPull(undefined);
    const ctl = runCreate(spec, base, { onSteps: setSteps, onPull: setPull });
    control.current = ctl;
    void ctl.done.then((r) => {
      control.current = undefined;
      setResult(r);
      if (r.ok && r.id) {
        toast.ok(recreate ? t('create.done.recreate', { name: spec.name }) : t('create.done.create', { name: spec.name || spec.image }));
        navigate({ view: 'container', id: r.id }, { replace: true });
      }
    });
  };

  const title = recreate ? t('create.editTitle', { name: spec.name }) : t('create.title');
  const sub = recreate ? t('create.editSub') : t('create.sub');
  const actions = recreate ? undefined : <Button icon="store" onClick={() => navigate({ view: 'templates' })}>{t('create.templates')}</Button>;
  const props = { spec, patch, showErrors };

  return (
    <>
      <PageHeader icon={recreate ? 'edit' : 'plus'} title={title} subtitle={sub} back actions={running ? undefined : actions} />
      {running ? (
        <RunView
          steps={steps}
          pull={pull}
          result={result}
          name={spec.name}
          recreate={recreate}
          onBack={() => { setRunning(false); setResult(undefined); }}
          onOpen={(id) => navigate({ view: 'container', id }, { replace: true })}
        />
      ) : (
        <>
          <ol className="dk-cr-stepbar" aria-label={t('create.steps')}>
            {STEPS.map((s, i) => (
              <li key={s}>
                <button type="button" className={`dk-cr-stp${i === step ? ' dk-cr-stp--on' : i < step || i <= reached ? ' dk-cr-stp--done' : ''}`} aria-current={i === step ? 'step' : undefined} onClick={() => jump(i)}>
                  <span>{i < step ? <Icon name="check" size={14} /> : i + 1}</span>
                  {t(`create.s.${s}`)}
                </button>
              </li>
            ))}
          </ol>
          <div className="dk-cr">
            <div className="dk-cr-forms">
              {step === 0 && <ImageStep {...props} problems={probs} recreate={recreate} />}
              {step === 1 && <StorageStep {...props} problems={probs} from={from} />}
              {step === 2 && <EnvStep {...props} problems={probs} />}
              {step === 3 && <NetworkStep {...props} problems={probs} />}
              {step === 4 && <ReviewStep {...props} recreate={recreate} />}
              {step === 3 && ncpu && spec.cpus && parseFloat(spec.cpus) > ncpu && <p className="dk-muted">{t('create.cpus.more', { n: ncpu })}</p>}
              <div className="dk-cr-nav">
                {step > 0 ? <Button variant="ghost" icon="chevronleft" onClick={() => go(step - 1)}>{t('common.back')}</Button> : <Button variant="ghost" onClick={() => navigate({ view: 'containers' })}>{t('common.cancel')}</Button>}
                <span className="dk-cr-sp" />
                {step < STEPS.length - 1 ? (
                  <Button variant="primary" onClick={next}>{t(`create.next.${STEPS[step + 1]}`)}</Button>
                ) : (
                  <Button variant="primary" icon="play" onClick={submit}>{recreate ? t('create.submit.recreate') : spec.start ? t('create.submit') : t('create.submit.noStart')}</Button>
                )}
              </div>
            </div>
            <aside className="dk-cr-side">
              <Summary spec={spec} title={t('create.sum.title')} />
            </aside>
          </div>
        </>
      )}
    </>
  );
}
