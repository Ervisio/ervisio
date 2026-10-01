import { useEffect, useMemo, useState } from 'react';
import { useT } from '../../i18n';
import { Button, Checkbox, EmptyState, Icon, Skeleton } from '../../ui';
import { formatBytes } from '../../lib/format';
import { Notes, SourceBadge, updateKey } from './helpers';
import { ScheduleDialog } from './ScheduleDialog';
import { checkNow, useSoftware } from './store';
import type { Update } from './types';

const FOLD = 6;

interface Props {
  actions: { busy: boolean; updateAll(): void; updateSome(list: Update[]): void; openTerminal(): void };
}

export function UpdatesTab({ actions }: Props) {
  const t = useT('software');
  const { summary, updates, checking, error } = useSoftware();
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [sched, setSched] = useState(false);

  // Every system package and Flatpak starts selected; AUR packages never do.
  useEffect(() => {
    setSel(new Set((updates ?? []).filter((u) => u.kind !== 'aur').map(updateKey)));
  }, [updates]);

  const groups = useMemo(() => {
    const u = updates ?? [];
    // Kernel, security and service-restart updates first, then by name.
    const weight = (x: Update) => (x.notes.includes('security') ? 0 : x.notes.includes('reboot') ? 1 : x.notes.length ? 2 : 3);
    const sorted = (k: Update['kind']) => u.filter((x) => x.kind === k).sort((a, b) => weight(a) - weight(b) || a.name.localeCompare(b.name));
    return {
      repo: sorted('repo'),
      aur: sorted('aur'),
      flatpak: sorted('flatpak'),
    };
  }, [updates]);

  if (!summary && !updates) {
    return error ? (
      <EmptyState icon="alert" title={t('error.load')} text={error.message} action={<Button onClick={() => void checkNow().catch(() => {})}>{t('checkNow')}</Button>} />
    ) : (
      <div className="sw-body"><Skeleton height={96} style={{ borderRadius: 22 }} /><Skeleton lines={5} height={46} style={{ marginTop: 18 }} /></div>
    );
  }

  const list = updates ?? [];
  const count = list.length;
  const selectable = list.filter((u) => u.kind !== 'aur');
  const chosen = selectable.filter((u) => sel.has(updateKey(u)));
  const partial = chosen.length > 0 && chosen.length < selectable.length;
  const size = selectable.reduce((a, u) => a + u.size, 0);
  const rebootNames = list.filter((u) => u.notes.includes('reboot')).map((u) => u.name);
  const securityNames = list.filter((u) => u.notes.includes('security')).map((u) => u.name);

  const toggle = (u: Update, on: boolean) =>
    setSel((s) => {
      const n = new Set(s);
      if (on) n.add(updateKey(u));
      else n.delete(updateKey(u));
      return n;
    });
  const setGroup = (g: Update[], on: boolean) =>
    setSel((s) => {
      const n = new Set(s);
      for (const u of g) {
        if (on) n.add(updateKey(u));
        else n.delete(updateKey(u));
      }
      return n;
    });

  const detail = [
    size > 0 ? t('hero.size', { size: formatBytes(size) }) : selectable.length === 0 ? t('hero.aurOnly') : '',
    rebootNames.length ? t('hero.reboot', { names: rebootNames.slice(0, 2).join(', '), count: Math.min(rebootNames.length, 2) }) : '',
    securityNames.length ? t('hero.security', { names: securityNames.slice(0, 2).join(', '), count: Math.min(securityNames.length, 2) }) : '',
  ]
    .filter(Boolean)
    .join(' ');

  const row = (u: Update, disabled = false) => (
    <div className={`sw-ur${sel.has(updateKey(u)) ? ' sw-ur--on' : ''}`} key={updateKey(u)}>
      <Checkbox checked={sel.has(updateKey(u))} disabled={disabled} onChange={(on) => toggle(u, on)} aria-label={t('select', { name: u.title || u.name })} />
      <div className="sw-nm">
        <b>{u.title || u.name}</b>
        <span className="sw-ver">
          {u.from ? (
            <>
              {u.from}
              <span className="sw-ar">→</span>
              <b>{u.to}</b>
            </>
          ) : (
            <>
              {t('newDependency')} <b>{u.to}</b>
            </>
          )}
          {u.title && <span className="sw-muted"> · {u.name}</span>}
        </span>
      </div>
      <SourceBadge kind={u.kind} source={u.source} />
      <span className="sw-notes"><Notes u={u} /></span>
      <span className="sw-sz">{u.size ? formatBytes(u.size) : ''}</span>
    </div>
  );

  const group = (id: 'repo' | 'aur' | 'flatpak', title: string, items: Update[]) => {
    if (!items.length) return null;
    const expanded = open[id];
    const shown = expanded ? items : items.slice(0, FOLD);
    const allOn = items.every((u) => sel.has(updateKey(u)));
    return (
      <section key={id} className="sw-group">
        <div className="sw-gh">
          <h3>{title}</h3>
          <span className="sw-muted">{items.length}</span>
          {id !== 'aur' && (
            <button type="button" className="sw-link" onClick={() => setGroup(items, !allOn)}>{allOn ? t('selectNone') : t('selectAll')}</button>
          )}
        </div>
        {id === 'aur' && (
          <div className="sw-aurw">
            <Icon name="alert" />
            <div>
              <div>{t('aur.warning')}</div>
              <small>{t('aur.terminal', { helper: summary?.aurHelper || 'yay' })}</small>
            </div>
            <Button size="sm" icon="terminal" onClick={actions.openTerminal}>{t('openTerminal')}</Button>
          </div>
        )}
        {shown.map((u) => row(u, id === 'aur'))}
        {items.length > FOLD && (
          <button type="button" className="sw-more" onClick={() => setOpen((o) => ({ ...o, [id]: !expanded }))}>
            {expanded ? t('showLess') : t('showMore', { count: items.length - FOLD })}
          </button>
        )}
      </section>
    );
  };

  const schedLabel = summary?.schedule ? t('schedule.at', { time: summary.schedule.at }) : t('schedule.tonight', { time: '03:00' });

  return (
    <div className="sw-body">
      {count === 0 ? (
        <div className="sw-hero sw-hero--ok">
          <span className="sw-hero-ic"><Icon name="check" /></span>
          <div>
            <b>{t('hero.upToDate')}</b>
            <small>{t('hero.upToDateText')}</small>
          </div>
          <div className="sw-hero-act">
            <Button icon="clock" onClick={() => setSched(true)}>{schedLabel}</Button>
            <Button icon="refresh" loading={checking} onClick={() => void checkNow().catch(() => {})}>{t('checkNow')}</Button>
          </div>
        </div>
      ) : (
        <div className="sw-hero">
          <span className="sw-hero-n">{count}</span>
          <div className="sw-hero-tx">
            <b>{t('hero.ready', { count })}</b>
            <small>{detail}</small>
          </div>
          <div className="sw-hero-act">
            <Button icon={summary?.schedule ? 'check' : 'clock'} onClick={() => setSched(true)}>{schedLabel}</Button>
            {partial && (
              <Button disabled={actions.busy} onClick={() => actions.updateSome(chosen)}>{t('updateSelected', { count: chosen.length })}</Button>
            )}
            <Button variant="primary" icon="download" disabled={actions.busy || selectable.length === 0} onClick={actions.updateAll}>{t('updateAll')}</Button>
          </div>
        </div>
      )}

      {summary && summary.warnings.length > 0 && (
        <div className="sw-warn"><Icon name="info" /><div>{summary.warnings.map((w) => <div key={w}>{w}</div>)}</div></div>
      )}
      {summary?.rebootPending && (
        <div className="sw-warn sw-warn--reboot"><Icon name="power" /><div>{t('rebootPending')}</div></div>
      )}
      {partial && summary?.manager === 'pacman' && (
        <div className="sw-warn"><Icon name="alert" /><div>{t('partial.hint')}</div></div>
      )}

      <div className="sw-list">
        {group('repo', t('group.repo'), groups.repo)}
        {group('aur', t('group.aur'), groups.aur)}
        {group('flatpak', t('group.flatpak'), groups.flatpak)}
      </div>

      <ScheduleDialog open={sched} onClose={() => setSched(false)} current={summary?.schedule?.at ?? null} />
    </div>
  );
}
