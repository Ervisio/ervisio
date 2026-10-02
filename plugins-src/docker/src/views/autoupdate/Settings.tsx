import { useMemo } from 'react';
import { containerName } from '../../api/format';
import { WATCHTOWER, WATCHTOWER_ONCE, cronOf, validCron } from '../../api/autoupdate';
import type { Container } from '../../api/types';
import { t } from '../../i18n';
import { Checkbox, Icon, Input, Segmented, Select, Switch } from '../../kit';
import { getSdk } from '../../sdk';
import type { AutoUpdateConfig } from '../../settings';

type Patch = (p: Partial<AutoUpdateConfig>) => void;

export const dayName = (d: number): string => new Intl.DateTimeFormat(getSdk().lang(), { weekday: 'long' }).format(new Date(2024, 0, 7 + d));
const pad = (n: number) => String(n).padStart(2, '0');
export const timeText = (c: AutoUpdateConfig): string => `${pad(c.schedule.hour)}:${pad(c.schedule.minute)}`;

export function scheduleSummary(c: AutoUpdateConfig): string {
  const s = c.schedule;
  if (s.type === 'daily') return t('autoupdate.schedule.sum.daily', { time: timeText(c) });
  if (s.type === 'weekly') return t('autoupdate.schedule.sum.weekly', { day: dayName(s.day), time: timeText(c) });
  return t('autoupdate.schedule.sum.custom');
}

export function ScheduleCard({ c, set }: { c: AutoUpdateConfig; set: Patch }) {
  const s = c.schedule;
  const setS = (p: Partial<AutoUpdateConfig['schedule']>) => set({ schedule: { ...s, ...p } });
  const days = useMemo(() => [0, 1, 2, 3, 4, 5, 6].map((d) => ({ value: String(d), label: dayName(d) })), []);
  const badCron = s.type === 'custom' && !validCron(s.cron);
  return (
    <section className="dk-card dk-au-card">
      <div className="dk-card-h"><h3><Icon name="clock" /> {t('autoupdate.schedule')}</h3></div>
      <Segmented
        aria-label={t('autoupdate.schedule')}
        value={s.type}
        options={[
          { value: 'daily', label: t('autoupdate.schedule.daily') },
          { value: 'weekly', label: t('autoupdate.schedule.weekly') },
          { value: 'custom', label: t('autoupdate.schedule.custom') },
        ]}
        onChange={(v) => setS({ type: v as AutoUpdateConfig['schedule']['type'], cron: v === 'custom' && !s.cron ? cronOf(c) : s.cron })}
      />
      {s.type !== 'custom' ? (
        <div className="dk-au-fields">
          {s.type === 'weekly' && <Select label={t('autoupdate.schedule.day')} value={String(s.day)} options={days} onChange={(v) => setS({ day: Number(v) })} />}
          <Input
            label={t('autoupdate.schedule.time')}
            type="time"
            value={timeText(c)}
            onChange={(e) => {
              const [h, m] = e.target.value.split(':').map(Number);
              if (Number.isFinite(h) && Number.isFinite(m)) setS({ hour: h, minute: m });
            }}
          />
        </div>
      ) : (
        <Input mono label={t('autoupdate.schedule.cron')} value={s.cron} placeholder="0 0 4 * * *" hint={t('autoupdate.schedule.cron.hint')} error={badCron ? t('autoupdate.schedule.cron.bad') : undefined} onChange={(e) => setS({ cron: e.target.value })} />
      )}
      <p className="dk-au-sub"><b>{scheduleSummary(c)}</b>. {t('autoupdate.schedule.tz')} <span className="dk-tag">{cronOf(c)}</span></p>
    </section>
  );
}

export function ScopeCard({ c, set, all }: { c: AutoUpdateConfig; set: Patch; all: Container[] }) {
  const names = useMemo(() => all.map(containerName).filter((n) => n !== WATCHTOWER && n !== WATCHTOWER_ONCE).sort(), [all]);
  const gone = c.containers.filter((n) => !names.includes(n));
  const toggle = (n: string, on: boolean) => set({ containers: on ? [...c.containers, n] : c.containers.filter((x) => x !== n) });
  return (
    <section className="dk-card dk-au-card">
      <div className="dk-card-h"><h3><Icon name="services" /> {t('autoupdate.scope')}</h3></div>
      <Segmented
        aria-label={t('autoupdate.scope')}
        value={c.mode}
        options={[
          { value: 'all', label: t('autoupdate.scope.all') },
          { value: 'choose', label: t('autoupdate.scope.choose') },
        ]}
        onChange={(v) => set({ mode: v as 'all' | 'choose' })}
      />
      <p className="dk-au-sub">{t(c.mode === 'all' ? 'autoupdate.scope.all.text' : 'autoupdate.scope.choose.text')}</p>
      {c.mode === 'choose' && (
        <>
          {names.length + gone.length === 0 && <p className="dk-au-sub">{t('autoupdate.scope.empty')}</p>}
          <div className="dk-au-pick">
            {[...names, ...gone].map((n) => (
              <label key={n} className={`dk-au-pk ${c.containers.includes(n) ? 'dk-au-pk--on' : ''}`}>
                <Checkbox checked={c.containers.includes(n)} aria-label={n} onChange={(v) => toggle(n, v)} />
                <span className="dk-mono">{n}</span>
                {gone.includes(n) && <small>{t('autoupdate.scope.gone')}</small>}
              </label>
            ))}
          </div>
          {c.containers.length === 0 && <p className="dk-au-warn">{t('autoupdate.scope.none')}</p>}
        </>
      )}
    </section>
  );
}

export function OptionsCard({ c, set }: { c: AutoUpdateConfig; set: Patch }) {
  const row = (key: 'cleanup' | 'includeStopped' | 'monitorOnly' | 'rolling', label: string, desc: string) => (
    <div className="dk-au-opt">
      <div>
        <b>{label}</b>
        <span>{desc}</span>
      </div>
      <Switch checked={c[key]} aria-label={label} onChange={(v) => set({ [key]: v } as Partial<AutoUpdateConfig>)} />
    </div>
  );
  return (
    <section className="dk-card dk-au-card">
      <div className="dk-card-h"><h3><Icon name="cog" /> {t('autoupdate.options')}</h3></div>
      {row('cleanup', t('autoupdate.opt.cleanup'), t('autoupdate.opt.cleanup.d'))}
      {row('includeStopped', t('autoupdate.opt.stopped'), t('autoupdate.opt.stopped.d'))}
      {row('monitorOnly', t('autoupdate.opt.monitor'), t('autoupdate.opt.monitor.d'))}
      {row('rolling', t('autoupdate.opt.rolling'), t('autoupdate.opt.rolling.d'))}
      <Input mono label={t('autoupdate.opt.image')} hint={t('autoupdate.opt.image.hint')} value={c.image} onChange={(e) => set({ image: e.target.value })} />
    </section>
  );
}
