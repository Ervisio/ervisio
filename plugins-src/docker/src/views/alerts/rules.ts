import { t } from '../../i18n';
import type { AlertRule } from '../../settings';

export const KINDS: AlertRule['kind'][] = ['stopped', 'restart-loop', 'unhealthy', 'cpu', 'memory', 'disk'];

export const KIND_ICON: Record<AlertRule['kind'], string> = {
  stopped: 'stop',
  'restart-loop': 'refresh',
  unhealthy: 'alert',
  cpu: 'cpu',
  memory: 'mem',
  disk: 'disk',
};

export const newId = (): string => `r${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`;

export function defaultRule(kind: AlertRule['kind']): AlertRule {
  const base: AlertRule = { id: newId(), kind, enabled: true, scope: 'all' };
  switch (kind) {
    case 'restart-loop':
      return { ...base, count: 3, minutes: 5 };
    case 'cpu':
      return { ...base, threshold: 90, minutes: 5 };
    case 'memory':
      return { ...base, threshold: 90, minutes: 5, memBasis: 'limit' };
    case 'disk':
      return { ...base, scope: 'all', threshold: 50 };
    default:
      return base;
  }
}

export const usualRules = (): AlertRule[] => (['stopped', 'restart-loop', 'unhealthy'] as const).map(defaultRule);

export function summary(r: AlertRule): string {
  switch (r.kind) {
    case 'restart-loop':
      return t('alerts.sum.restart-loop', { n: r.count ?? 3, m: r.minutes ?? 5 });
    case 'cpu':
      return t('alerts.sum.cpu', { pct: r.threshold ?? 90, m: r.minutes ?? 5 });
    case 'memory':
      return t(r.memBasis === 'host' ? 'alerts.sum.memory.host' : 'alerts.sum.memory.limit', { pct: r.threshold ?? 90, m: r.minutes ?? 5 });
    case 'disk':
      return t('alerts.sum.disk', { gb: r.threshold ?? 50 });
    default:
      return t(`alerts.sum.${r.kind}`);
  }
}

export function scopeText(r: AlertRule): string {
  if (r.kind === 'disk') return t('alerts.scope.disk');
  if (r.scope === 'stack' && r.target) return t('alerts.scope.stack', { name: r.target });
  if (r.scope === 'container' && r.target) return t('alerts.scope.container', { name: r.target });
  if (!r.scope && r.containers?.length) return r.containers.join(', ');
  return t('alerts.scope.all');
}

const pos = (n: number | undefined) => typeof n === 'number' && isFinite(n) && n > 0;

export function valid(r: AlertRule): boolean {
  if (r.kind !== 'disk' && r.scope && r.scope !== 'all' && !r.target) return false;
  switch (r.kind) {
    case 'restart-loop':
      return pos(r.count) && pos(r.minutes);
    case 'cpu':
    case 'memory':
      return pos(r.threshold) && (r.minutes ?? 0) >= 0 && r.minutes !== undefined;
    case 'disk':
      return pos(r.threshold);
    default:
      return true;
  }
}
