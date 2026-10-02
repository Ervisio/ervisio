import { useState } from 'react';
import { containerAction, runBulk } from '../../api/actions';
import type { StackService } from '../../api/compose';
import { localStatus, shortImage } from '../../api/format';
import { containers } from '../../api/resources';
import { t } from '../../i18n';
import { IconButton, toast } from '../../kit';
import { navigate } from '../../router';
import { StatusDot } from '../../ui/StatusDot';

/** Services of a stack with their state; a row opens the container page, the buttons start, stop or restart it. */
export function ServicesCard({ services, images }: { services: StackService[]; images: Record<string, string> }) {
  const [busy, setBusy] = useState<string | null>(null);
  const act = async (s: StackService, action: 'start' | 'stop' | 'restart') => {
    setBusy(s.name);
    const failed = await runBulk(s.containers.map((c) => c.Id), (id) => containerAction(id, action));
    setBusy(null);
    if (failed.length) toast.err(t(`containers.fail.${action}`, { name: s.name }), failed[0].message);
    void containers.refresh();
  };
  if (!services.length) return <p className="dk-muted">{t('stacks.noServices')}</p>;
  return (
    <div className="dk-sk-svcs">
      {services.map((s) => {
        const first = s.containers[0];
        const up = s.state === 'running' || s.state === 'restarting' || s.state === 'paused';
        const img = first ? shortImage(first.Image) : shortImage(images[s.name] ?? '');
        return (
          <div key={s.name} className={`dk-sk-sv${first ? ' dk-sk-sv--link' : ''}`}>
            <button
              type="button"
              className="dk-sk-sv-main"
              disabled={!first}
              title={first ? t('stacks.openContainer', { name: s.name }) : undefined}
              onClick={() => first && navigate({ view: 'container', id: first.Id })}
            >
              <StatusDot state={s.state === 'none' ? 'created' : s.state === 'stopped' ? 'exited' : s.state} />
              <span className="dk-sk-sv-tx">
                <b>{s.name}{s.containers.length > 1 && <small> ×{s.containers.length}</small>}</b>
                <span className="dk-sk-img">{img}</span>
              </span>
              <span className="dk-muted dk-sk-sv-st">{first ? localStatus(first.Status) : t('stacks.notCreated')}</span>
            </button>
            {first && (
              <span className="dk-sk-sv-act">
                {up ? <IconButton icon="stop" label={t('common.stop')} loading={busy === s.name} onClick={() => act(s, 'stop')} /> : <IconButton icon="play" label={t('common.start')} loading={busy === s.name} onClick={() => act(s, 'start')} />}
                <IconButton icon="refresh" label={t('common.restart')} disabled={busy === s.name} onClick={() => act(s, 'restart')} />
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
