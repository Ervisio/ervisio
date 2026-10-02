import { countContainers } from '../api/model';
import { containers } from '../api/resources';
import { t } from '../i18n';
import { Button, StatCard } from '../kit';
import { getSdk } from '../sdk';

/** Overview widget `containers`: running of total, how many need attention, and an Open button. */
export function ContainersWidget() {
  const { data, error } = containers.use();
  const c = data ? countContainers(data) : undefined;
  const sub = error && !data ? error.message : !c ? '' : c.problems ? t('containers.widget.problems', { n: c.problems }) : c.total ? t('containers.widget.allWell') : t('containers.sub.none');
  return (
    <div>
      <StatCard
        hue="file"
        icon="box"
        label={t('containers.title')}
        value={c ? `${c.running}/${c.total}` : error ? '–' : '…'}
        unit={c ? ` ${t('containers.widget.running')}` : undefined}
        sub={sub}
        onClick={() => getSdk().open('docker')}
      />
      <div className="dk-w-open">
        <Button size="sm" icon="externallink" onClick={() => getSdk().open('docker')}>{t('containers.widget.open')}</Button>
      </div>
    </div>
  );
}
