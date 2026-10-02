import type { Container } from '../api/types';
import { healthOf } from '../api/model';
import { t } from '../i18n';
import { Badge, type Tone } from '../kit';

/** Colour class of a container state: ok, warn, info, err or n (stopped). */
export function stateTone(state: string, unhealthy = false): 'ok' | 'warn' | 'info' | 'err' | 'n' {
  if (unhealthy) return 'err';
  switch (state) {
    case 'running':
      return 'ok';
    case 'restarting':
      return 'warn';
    case 'paused':
      return 'info';
    case 'dead':
      return 'err';
    default:
      return 'n';
  }
}

/** The round state indicator used on cards and rows. */
export function StatusDot({ state, unhealthy }: { state: string; unhealthy?: boolean }) {
  return <span className={`dk-dot dk-dot--${stateTone(state, unhealthy)}`} title={t(`state.${state}`)} />;
}

/** State and health as badges. */
export function StateBadges({ container }: { container: Container }) {
  const h = healthOf(container);
  const tone = stateTone(container.State);
  const map: Record<string, Tone> = { ok: 'ok', warn: 'warn', info: 'info', err: 'err', n: 'neutral' };
  return (
    <>
      <Badge tone={map[tone]}>{t(`state.${container.State}`)}</Badge>
      {h === 'unhealthy' && <Badge tone="err">{t('health.unhealthy')}</Badge>}
    </>
  );
}
