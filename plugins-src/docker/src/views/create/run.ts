/**
 * Runs the create (or recreate) of a container as a list of steps the page can show.
 * Recreate: pull if asked, stop, rename the old one to <name>-old, create, start, remove the old one.
 * Any failure after the rename puts things back: the new container is removed, the old one gets its name back
 * and is started again when it was running.
 */
import { containerAction, removeContainer } from '../../api/actions';
import { docker } from '../../api/engine';
import { containers } from '../../api/resources';
import { createBody, type Base, type Spec } from './model';
import { localImage, pullImage, type PullHandle, type PullState } from './pull';

export type StepId = 'pull' | 'stop' | 'rename' | 'create' | 'connect' | 'start' | 'cleanup';
export type StepStatus = 'todo' | 'run' | 'done' | 'fail' | 'skip';
export interface Step {
  id: StepId;
  status: StepStatus;
  detail?: string;
}

export interface RunHooks {
  onSteps(steps: Step[]): void;
  onPull(p: PullState): void;
}

export interface RunResult {
  ok: boolean;
  id?: string;
  error?: string;
  /** True when a failed recreate was undone. */
  rolledBack?: boolean;
  rollbackError?: string;
}

export interface RunControl {
  done: Promise<RunResult>;
  cancel(): void;
}

const enc = encodeURIComponent;

export function plannedSteps(spec: Spec, base: Base | undefined, imageLocal: boolean | undefined): Step[] {
  const s: Step[] = [];
  s.push({ id: 'pull', status: spec.pullFirst || imageLocal === false ? 'todo' : 'skip' });
  if (base) {
    s.push({ id: 'stop', status: base.wasRunning ? 'todo' : 'skip' });
    s.push({ id: 'rename', status: 'todo' });
  }
  s.push({ id: 'create', status: 'todo' });
  if (base && base.extraNetworks.length) s.push({ id: 'connect', status: 'todo' });
  s.push({ id: 'start', status: spec.start ? 'todo' : 'skip' });
  if (base) s.push({ id: 'cleanup', status: 'todo' });
  return s;
}

export function runCreate(spec: Spec, base: Base | undefined, hooks: RunHooks): RunControl {
  let pull: PullHandle | undefined;
  let cancelled = false;
  const steps = plannedSteps(spec, base, undefined);
  const set = (id: StepId, status: StepStatus, detail?: string) => {
    const s = steps.find((x) => x.id === id);
    if (s) {
      s.status = status;
      s.detail = detail;
    }
    hooks.onSteps(steps.map((x) => ({ ...x })));
  };

  const done = (async (): Promise<RunResult> => {
    const oldId = base?.inspect.Id;
    const oldName = base?.inspect.Name.replace(/^\//, '') ?? '';
    let stopped = false;
    let renamedTo = '';
    let newId = '';
    let current: StepId = 'pull';
    try {
      // Pull when asked, or when the image is not here.
      let img = await localImage(spec.image).catch(() => undefined);
      steps.find((s) => s.id === 'pull')!.status = spec.pullFirst || !img ? 'todo' : 'skip';
      hooks.onSteps(steps.map((x) => ({ ...x })));
      if (steps.find((s) => s.id === 'pull')!.status === 'todo') {
        set('pull', 'run');
        pull = pullImage(spec.image, hooks.onPull);
        await pull.done;
        set('pull', 'done');
        img = await localImage(spec.image).catch(() => undefined);
      }
      if (cancelled) throw new Error('cancelled');
      const full: Base | undefined = base && { ...base, imageConfig: base.imageConfig };
      const body = createBody(spec, full);

      if (base && oldId) {
        if (base.wasRunning) {
          current = 'stop';
          set('stop', 'run');
          await containerAction(oldId, 'stop');
          stopped = true;
          set('stop', 'done');
        }
        current = 'rename';
        set('rename', 'run');
        let tmp = `${oldName}-old`;
        const taken = new Set((containers.peek().data ?? []).flatMap((c) => c.Names.map((n) => n.replace(/^\//, ''))));
        if (taken.has(tmp)) tmp = `${tmp}-${Date.now().toString(36)}`;
        await docker.post(`/containers/${oldId}/rename`, { name: tmp });
        renamedTo = tmp;
        set('rename', 'done', tmp);
      }

      current = 'create';
      set('create', 'run');
      const created = await docker.post<{ Id: string }>('/containers/create', spec.name.trim() ? { name: spec.name.trim() } : undefined, body);
      newId = created.Id;
      set('create', 'done');

      if (base?.extraNetworks.length) {
        current = 'connect';
        set('connect', 'run');
        for (const n of base.extraNetworks) await docker.post(`/networks/${enc(n)}/connect`, undefined, { Container: newId });
        set('connect', 'done');
      }
      if (spec.start) {
        current = 'start';
        set('start', 'run');
        await containerAction(newId, 'start');
        set('start', 'done');
      }
      if (base && oldId) {
        current = 'cleanup';
        set('cleanup', 'run');
        await removeContainer(oldId, { force: true, volumes: false });
        set('cleanup', 'done');
      }
      void containers.refresh();
      return { ok: true, id: newId };
    } catch (e) {
      const error = (e as Error).message || String(e);
      set(current, 'fail', error);
      const out: RunResult = { ok: false, error };
      if (base && oldId && (renamedTo || stopped)) {
        try {
          if (newId) await removeContainer(newId, { force: true, volumes: false }).catch(() => undefined);
          if (renamedTo) await docker.post(`/containers/${oldId}/rename`, { name: oldName });
          if (stopped) await containerAction(oldId, 'start');
          out.rolledBack = true;
        } catch (re) {
          out.rollbackError = (re as Error).message;
        }
      } else if (newId) {
        // A plain create that failed after the container exists (for example it could not start): keep it, the
        // user can look at its logs.
        out.id = newId;
      }
      void containers.refresh();
      return out;
    }
  })();

  return {
    done,
    cancel() {
      cancelled = true;
      pull?.cancel();
    },
  };
}
