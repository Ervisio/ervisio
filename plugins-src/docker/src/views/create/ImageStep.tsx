import { useEffect, useMemo, useRef, useState } from 'react';
import { errorText } from '../../api/engine';
import { formatBytes, relativeTime } from '../../api/format';
import { images } from '../../api/resources';
import { t } from '../../i18n';
import { Button, Input, Progress, Select, Switch } from '../../kit';
import { Hint, Section, type StepProps } from './parts';
import type { Problems, Restart } from './model';
import { localImage, pullImage, type LocalImage, type PullHandle, type PullState } from './pull';

type Check = { state: 'idle' } | { state: 'checking' } | { state: 'local'; img: LocalImage } | { state: 'missing' } | { state: 'error'; message: string };

export function ImageStep({ spec, patch, showErrors, problems, recreate }: StepProps & { problems: Problems; recreate: boolean }) {
  const { data: imgs } = images.use();
  const [check, setCheck] = useState<Check>({ state: 'idle' });
  const [pull, setPull] = useState<PullState | undefined>();
  const [pulling, setPulling] = useState(false);
  const [pullError, setPullError] = useState('');
  const handle = useRef<PullHandle | undefined>();
  const [tick, setTick] = useState(0);
  const ref = spec.image.trim();

  useEffect(() => {
    if (!ref || /\s/.test(ref)) {
      setCheck({ state: 'idle' });
      return;
    }
    setCheck({ state: 'checking' });
    let live = true;
    const timer = setTimeout(() => {
      localImage(ref).then(
        (img) => live && setCheck(img ? { state: 'local', img } : { state: 'missing' }),
        (e) => live && setCheck({ state: 'error', message: errorText(e) }),
      );
    }, 350);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [ref, tick]);

  useEffect(() => () => handle.current?.cancel(), []);

  const doPull = async () => {
    setPulling(true);
    setPullError('');
    setPull({ status: '', layers: 0, doneLayers: 0 });
    handle.current = pullImage(ref, setPull);
    try {
      await handle.current.done;
      setTick((n) => n + 1);
    } catch (e) {
      setPullError((e as Error).message === 'cancelled' ? '' : errorText(e));
    } finally {
      setPulling(false);
      handle.current = undefined;
    }
  };

  const local = useMemo(() => {
    const out: { tag: string; size: number; created: number }[] = [];
    for (const i of imgs ?? []) for (const tag of i.RepoTags ?? []) if (tag !== '<none>:<none>') out.push({ tag, size: i.Size, created: i.Created });
    const q = ref.toLowerCase();
    return out.filter((x) => !q || x.tag.toLowerCase().includes(q)).filter((x) => x.tag !== ref).sort((a, b) => a.tag.localeCompare(b.tag)).slice(0, 8);
  }, [imgs, ref]);

  const imgErr = showErrors && problems.image ? t(problems.image) : undefined;
  const nameErr = (showErrors || (spec.name && problems.name)) && problems.name ? t(problems.name) : undefined;

  return (
    <>
      <Section title={t('create.image')}>
        <Input
          mono
          icon="image"
          value={spec.image}
          onChange={(e) => patch({ image: e.target.value })}
          placeholder="nginx:1.27"
          spellCheck={false}
          autoCapitalize="off"
          autoComplete="off"
          error={imgErr}
          aria-label={t('create.image')}
        />
        {check.state === 'local' && (
          <Hint tone="ok" icon="check">{t('create.img.local', { size: formatBytes(check.img.Size), age: relativeTime(check.img.Created) })}</Hint>
        )}
        {check.state === 'missing' && !pulling && <Hint tone="warn" icon="download">{t('create.img.missing')}</Hint>}
        {check.state === 'checking' && <Hint>{t('create.img.checking')}</Hint>}
        {check.state === 'error' && <Hint tone="err" icon="alert">{check.message}</Hint>}
        {pullError && <Hint tone="err" icon="alert">{t('create.pull.failed', { message: pullError })}</Hint>}

        {pulling && pull && (
          <div className="dk-cr-pull">
            <Progress value={pull.percent} label={t('create.pull.progress')} />
            <span className="dk-muted">{pull.percent !== undefined ? `${pull.percent}%` : ''} {pull.layers ? t('create.pull.layers', { done: pull.doneLayers, total: pull.layers }) : t('create.pull.starting')} {pull.status}</span>
            <Button size="sm" onClick={() => handle.current?.cancel()}>{t('common.cancel')}</Button>
          </div>
        )}
        {!pulling && ref && !/\s/.test(ref) && (check.state === 'missing' || check.state === 'local') && (
          <div className="dk-cr-row">
            <Button size="sm" icon="download" onClick={() => void doPull()}>{check.state === 'local' ? t('create.pull.again') : t('create.pull.now')}</Button>
          </div>
        )}
        {check.state === 'local' && (
          <Switch checked={spec.pullFirst} onChange={(v) => patch({ pullFirst: v })} label={recreate ? t('create.pullFirst.recreate') : t('create.pullFirst')} />
        )}

        {local.length > 0 && (
          <div className="dk-cr-local">
            <span className="dk-muted">{t('create.local')}</span>
            <div className="dk-cr-chips">
              {local.map((x) => (
                <button type="button" key={x.tag} className="dk-cr-pick" onClick={() => patch({ image: x.tag })}>
                  <span className="dk-mono">{x.tag}</span>
                  <small>{formatBytes(x.size)}</small>
                </button>
              ))}
            </div>
          </div>
        )}
      </Section>

      <Section title={t('create.basics')}>
        <div className="dk-cr-g2">
          <Input
            label={t('create.name')}
            mono
            value={spec.name}
            onChange={(e) => patch({ name: e.target.value })}
            placeholder={recreate ? '' : t('create.name.ph')}
            error={nameErr}
            hint={t('create.name.hint')}
            spellCheck={false}
            autoCapitalize="off"
          />
          <Select
            label={t('create.restart')}
            value={spec.restart}
            onChange={(v) => patch({ restart: v as Restart })}
            hint={t(`create.restart.${spec.restart}.hint`)}
            options={(['unless-stopped', 'always', 'on-failure', 'no'] as Restart[]).map((v) => ({ value: v, label: t(`create.restart.${v}`) }))}
          />
        </div>
      </Section>
    </>
  );
}
