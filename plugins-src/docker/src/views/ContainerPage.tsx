import { useState } from 'react';
import { containerAction, removeContainer, type ContainerAction } from '../api/actions';
import { resetEngine } from '../api/engine';
import { formatDuration, shortImage } from '../api/format';
import { useStats } from '../api/hooks';
import { COMPOSE_PROJECT } from '../api/types';
import { t } from '../i18n';
import { Badge, Button, Checkbox, EmptyState, Icon, IconButton, Skeleton, toast, type Tone } from '../kit';
import { back, navigate, type RouteProps } from '../router';
import { ErrorState } from '../ui/ErrorState';
import { stateTone } from '../ui/StatusDot';
import { InspectTab } from './container/InspectTab';
import { LogsTab } from './container/Logs';
import { Overview } from './container/Overview';
import { SettingsTab } from './container/SettingsTab';
import { ShellTab } from './container/ShellTab';
import { StatsTab } from './container/StatsTab';
import { TypeConfirm } from './container/TypeConfirm';
import { useInspect } from './container/useInspect';
import { nameOf, useNow } from './container/util';
import { classify } from '../api/engine';

type Tab = NonNullable<RouteProps<'container'>['tab']>;
const TABS: { id: Tab; icon: string }[] = [
  { id: 'overview', icon: 'overview' },
  { id: 'logs', icon: 'logs' },
  { id: 'stats', icon: 'cpu' },
  { id: 'shell', icon: 'terminal' },
  { id: 'inspect', icon: 'code' },
  { id: 'settings', icon: 'cog' },
];
const TONES: Record<string, Tone> = { ok: 'ok', warn: 'warn', info: 'info', err: 'err', n: 'neutral' };

/** Container detail page (design 026 a): header with actions, then Overview / Logs / Stats / Shell / Inspect / Settings. */
export function ContainerPage({ id, tab }: RouteProps<'container'>) {
  const { data: c, error, loading, reload } = useInspect(id);
  const [busy, setBusy] = useState<ContainerAction | null>(null);
  const [removing, setRemoving] = useState(false);
  const [rmVolumes, setRmVolumes] = useState(false);
  const [visited, setVisited] = useState<Set<Tab>>(new Set());
  // The shell remounts a view whenever its route changes, so tab clicks stay in local state (the route's tab is the first tab).
  const [picked, setTab] = useState<Tab>(tab && TABS.some((x) => x.id === tab) ? tab : 'overview');
  const active = picked;
  useNow(15000);

  const running = !!c?.State.Running;
  // Keeps the shared stats stream (and its history) alive while the page is open, whichever tab shows.
  useStats(c?.Id, running, 10000);

  if (!visited.has(active)) setVisited(new Set(visited).add(active));

  const head = (
    <Button className="dk-c-back" variant="ghost" size="sm" icon="chevronleft" onClick={back}>{t('container.back')}</Button>
  );

  if (!c) {
    if (error) {
      const info = classify(error);
      const gone = (error as { status?: number }).status === 404;
      return (
        <div className="dk-c-ct">
          {head}
          {gone ? (
            <EmptyState icon="box" hue="file" title={t('container.gone.title')} text={t('container.gone.text')} action={<Button variant="primary" onClick={() => navigate({ view: 'containers' }, { root: true })}>{t('container.gone.action')}</Button>} />
          ) : <ErrorState error={info} onRetry={() => { resetEngine(); reload(); }} />}
        </div>
      );
    }
    return <div className="dk-c-ct">{head}<Skeleton height={84} style={{ borderRadius: 18 }} /><Skeleton height={44} style={{ borderRadius: 14, marginTop: 12 }} /><Skeleton height={260} style={{ borderRadius: 18, marginTop: 12 }} /></div>;
  }

  const name = nameOf(c.Name);
  const state = c.State.Status;
  const health = c.State.Health?.Status;
  const unhealthy = health === 'unhealthy';
  const tone = TONES[stateTone(state, false)];
  const stack = c.Config.Labels?.[COMPOSE_PROJECT];
  const paused = c.State.Paused;
  const up = running && c.State.StartedAt ? formatDuration((Date.now() - Date.parse(c.State.StartedAt)) / 1000) : '';

  const act = async (action: ContainerAction) => {
    setBusy(action);
    try {
      await containerAction(c.Id, action);
      toast.ok(t(`container.done.${action}`, { name }));
      reload();
    } catch (e) {
      toast.err(t(`container.fail.${action}`, { name }), (e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const doRemove = async () => {
    await removeContainer(c.Id, { force: true, volumes: rmVolumes });
    toast.ok(t('containers.done.remove', { name }));
    navigate({ view: 'containers' }, { root: true });
  };

  return (
    <div className="dk-c-ct">
      {head}
      <header className="dk-c-ch">
        <span className="dk-c-ch-ic"><Icon name="box" /></span>
        <div className="dk-c-ch-tx">
          <h1>
            <span className="dk-c-ch-name" title={name}>{name}</span>
            <Badge tone={tone} dot>{t(`state.${state}`)}</Badge>
            {health && health !== 'none' && <Badge tone={unhealthy ? 'err' : health === 'healthy' ? 'ok' : 'warn'}>{t(`health.${health}`)}</Badge>}
          </h1>
          <p>
            <span className="dk-mono dk-c-ch-img" title={c.Config.Image}>{c.Config.Image.length > 60 ? shortImage(c.Config.Image) : c.Config.Image}</span>
            {stack && (
              <>
                <span className="dk-muted">{t('container.inStack')}</span>
                <button type="button" className="dk-c-link" onClick={() => navigate({ view: 'stack', name: stack })}><Icon name="grid" />{stack}</button>
              </>
            )}
            {up && <span className="dk-muted">{t('container.up', { time: up })}</span>}
          </p>
        </div>
        <div className="dk-c-ch-act">
          {running || state === 'restarting' || paused ? (
            <>
              <Button icon={paused ? 'play' : 'pause'} loading={busy === 'pause' || busy === 'unpause'} disabled={!!busy || state === 'restarting'} onClick={() => act(paused ? 'unpause' : 'pause')}>{paused ? t('container.resume') : t('common.pause')}</Button>
              <Button icon="stop" loading={busy === 'stop'} disabled={!!busy} onClick={() => act('stop')}>{t('common.stop')}</Button>
              <Button icon="refresh" loading={busy === 'restart'} disabled={!!busy} onClick={() => act('restart')}>{t('common.restart')}</Button>
            </>
          ) : (
            <Button variant="primary" icon="play" loading={busy === 'start'} disabled={!!busy} onClick={() => act('start')}>{t('common.start')}</Button>
          )}
          <Button icon="edit" onClick={() => navigate({ view: 'create', from: c.Id })}>{t('container.edit')}</Button>
          <IconButton icon="trash" variant="danger" label={t('common.remove')} onClick={() => setRemoving(true)} />
        </div>
      </header>

      <div className="dk-c-ctabs" role="tablist" aria-label={name}>
        {TABS.map((x) => (
          <button key={x.id} type="button" role="tab" aria-selected={active === x.id} className={active === x.id ? 'on' : ''} onClick={() => setTab(x.id)}>
            <Icon name={x.icon} />{t(`container.tab.${x.id}`)}
          </button>
        ))}
      </div>

      {active === 'overview' && <Overview id={c.Id} inspect={c} running={running} />}
      {active === 'logs' && <LogsTab id={c.Id} name={name} tty={!!c.Config.Tty} running={running} />}
      {active === 'stats' && <StatsTab id={c.Id} inspect={c} running={running} />}
      {visited.has('shell') && (
        <div hidden={active !== 'shell'}>
          <ShellTab id={c.Id} running={running} visible={active === 'shell'} onStart={() => act('start')} />
        </div>
      )}
      {active === 'inspect' && <InspectTab id={c.Id} inspect={c} />}
      {active === 'settings' && <SettingsTab inspect={c} onChanged={reload} />}
      {loading && <span className="dk-c-sr" aria-live="polite">{t('common.loading')}</span>}

      <TypeConfirm
        open={removing}
        onClose={() => { setRemoving(false); setRmVolumes(false); }}
        onConfirm={doRemove}
        title={t('containers.removeTitle', { name })}
        description={t('containers.removeDesc')}
        confirmLabel={t('common.remove')}
        confirmText={name}
      >
        <Checkbox checked={rmVolumes} onChange={setRmVolumes} label={t('containers.removeVolumes')} />
      </TypeConfirm>
    </div>
  );
}
