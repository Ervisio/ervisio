import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError, call, useSession } from '../../api';
import { useT } from '../../i18n';
import { formatBytes, formatDuration } from '../../lib/format';
import { usePolling, useRefreshInterval } from '../../lib/refresh';
import { Badge, Button, ConfirmDialog, EmptyState, Icon, Panel, Skeleton, Sparkline, Switch, Tabs, Textarea, toast } from '../../ui';
import { bootMode, formatClockTime, short, stateKey, stateTone } from './helpers';
import type { Action, Detail, LogLine, PanelTab, Unit, UnitFile } from './types';

interface Props {
  name: string;
  tab: PanelTab;
  onTab(t: PanelTab): void;
  onClose(): void;
  inline: boolean;
  /** Bumped by the page after a state change so the panel refreshes at once. */
  rev: number;
  row?: Unit;
  run(name: string, action: Action): void;
  busy: boolean;
  onOpenUnit(name: string): void;
  onChanged(): void;
}

const MAX_SAMPLES = 60;

export function UnitPanel({ name, tab, onTab, onClose, inline, rev, row, run, busy, onOpenUnit, onChanged }: Props) {
  const t = useT('services');
  const win = useSession().session?.os === 'windows';
  const [detail, setDetail] = useState<Detail | null>(null);
  const [err, setErr] = useState<ApiError | null>(null);
  const [mem, setMem] = useState<number[]>([]);

  const load = useCallback(async () => {
    try {
      const d = await call<Detail>('services.get', { name });
      setDetail(d);
      setErr(null);
      if (d.memory != null) setMem((m) => [...m, d.memory as number].slice(-MAX_SAMPLES));
    } catch (e) {
      if (e instanceof ApiError) setErr(e);
    }
  }, [name]);

  // Poll while the panel is open: this is also what feeds the memory sparkline.
  useEffect(() => {
    void load();
  }, [load]);
  usePolling(() => void load(), Math.max(useRefreshInterval(), 1000));
  useEffect(() => {
    if (rev > 0) void load();
  }, [rev, load]);

  const u: Unit | undefined = detail ?? row;
  const running = !!u && (u.state === 'running' || u.state === 'finished');

  const subtitle = u
    ? u.state === 'running'
      ? u.since
        ? t('since.running', { time: formatDuration(Date.now() / 1000 - u.since) })
        : t('state.running')
      : u.since && u.state !== 'stopped'
        ? t(`since.${u.state}`, { time: formatDuration(Date.now() / 1000 - u.since) })
        : t(`state.${stateKey(u)}`)
    : undefined;

  const tabs = (
    <>
      <div className="svc-pacts">
        <Button icon="refresh" variant="primary" loading={busy} onClick={() => run(name, 'restart')}>{t('actions.restart')}</Button>
        {win ? (
          u?.sub === 'paused' ? <Button icon="play" disabled={busy} onClick={() => run(name, 'continue')}>{t('actions.continue')}</Button> : <Button disabled={busy || !running} onClick={() => run(name, 'pause')}>{t('actions.pause')}</Button>
        ) : (
          <Button disabled={busy || !running} onClick={() => run(name, 'reload')}>{t('actions.reload')}</Button>
        )}
        {running ? <Button icon="power" disabled={busy} onClick={() => run(name, 'stop')}>{t('actions.stop')}</Button> : <Button icon="play" disabled={busy} onClick={() => run(name, 'start')}>{t('actions.start')}</Button>}
      </div>
      <Tabs
        variant="underline"
        hue="svc"
        value={tab}
        onChange={onTab}
        aria-label={t('panel.tabs')}
        items={[
          { id: 'info' as const, label: t('panel.info') },
          ...(win ? [] : [{ id: 'logs' as const, label: t('panel.logs') }, { id: 'unit' as const, label: t('panel.unitFile') }]),
          { id: 'deps' as const, label: t('panel.deps') },
        ]}
      />
    </>
  );

  return (
    <Panel open onClose={onClose} title={short(name)} subtitle={subtitle} icon="services" hue="svc" inline={inline} tabs={tabs}>
      {err && !detail ? (
        <EmptyState icon="alert" hue="svc" title={t('panel.notFound')} text={err.message} />
      ) : !detail && !row ? (
        <Skeleton lines={6} height={22} />
      ) : tab === 'info' ? (
        <InfoTab name={name} u={u!} detail={detail} mem={mem} run={run} busy={busy} win={win} />
      ) : tab === 'logs' ? (
        <LogsTab name={name} />
      ) : tab === 'unit' ? (
        <UnitFileTab name={name} onSaved={() => { void load(); onChanged(); }} />
      ) : (
        <DepsTab detail={detail} onOpenUnit={onOpenUnit} />
      )}
    </Panel>
  );
}

function InfoTab({ name, u, detail, mem, run, busy, win }: { name: string; u: Unit; detail: Detail | null; mem: number[]; run(n: string, a: Action): void; busy: boolean; win: boolean }) {
  const t = useT('services');
  const mode = bootMode(u.enabled);
  const lo = mem.length ? Math.min(...mem) : 0;
  const hi = mem.length ? Math.max(...mem) : 0;
  const pad = Math.max((hi - lo) * 0.1, hi * 0.02, 1);
  return (
    <>
      <dl className="svc-kv">
        <dt>{t('info.status')}</dt>
        <dd><Badge tone={stateTone(u)}>{t(`state.${stateKey(u)}`)}</Badge></dd>
        <dt>{t('info.pid')}</dt>
        <dd className="svc-mono">{u.pid || '—'}</dd>
        <dt>{t('info.memory')}</dt>
        <dd className="svc-mono">{u.memory != null ? formatBytes(u.memory) : '—'}</dd>
      </dl>
      {mem.length > 1 && (
        <div className="svc-spark" aria-label={t('info.memoryHistory')}>
          <Sparkline values={mem} height={56} min={lo - pad} max={hi + pad} />
          <small className="svc-muted">{t('info.sparkNote')}</small>
        </div>
      )}
      <div className="svc-tg">
        <div>
          <b>{t('info.boot')}</b>
          <small>{mode === 'fixed' ? t('info.bootFixed', { state: u.enabled ? (win ? t(`win.boot.${u.enabled}`) : t(`boot.${u.enabled}`)) : t('boot.unknown') }) : t('info.bootHint')}</small>
        </div>
        {mode !== 'fixed' && <Switch checked={mode === 'on'} disabled={busy} onChange={() => run(name, mode === 'on' ? 'disable' : 'enable')} aria-label={t('boot.label', { name: short(name) })} />}
      </div>
      <dl className="svc-kv">
        <dt>{t('info.loadedFrom')}</dt>
        <dd className="svc-mono svc-wrap">{detail?.path || '—'}</dd>
        {!win && (
          <>
            <dt>{t('info.group')}</dt>
            <dd>{t(`purpose.${u.purpose}`)}</dd>
          </>
        )}
        {detail?.description && (
          <>
            <dt>{t('info.description')}</dt>
            <dd>{detail.description}</dd>
          </>
        )}
        {detail && detail.result && detail.result !== 'success' && (
          <>
            <dt>{t('info.lastResult')}</dt>
            <dd>{detail.result}{detail.exitCode ? ` (${detail.exitCode})` : ''}</dd>
          </>
        )}
      </dl>
      {(win ? u.enabled !== 'masked' : u.load !== 'masked') ? (
        <Button variant="ghost" className="svc-self" onClick={() => run(name, 'mask')}>{win ? t('win.mask') : t('actions.mask')}</Button>
      ) : (
        <Button variant="ghost" className="svc-self" onClick={() => run(name, 'unmask')}>{win ? t('win.unmask') : t('actions.unmask')}</Button>
      )}
    </>
  );
}

function LogsTab({ name }: { name: string }) {
  const t = useT('services');
  const nav = useNavigate();
  const [lines, setLines] = useState<LogLine[] | null>(null);
  const [err, setErr] = useState('');
  const box = useRef<HTMLPreElement>(null);

  useEffect(() => {
    let live = true;
    const go = async () => {
      try {
        const r = await call<{ lines: LogLine[] }>('services.logs', { name, lines: 200 });
        if (live) {
          setLines(r.lines);
          setErr('');
        }
      } catch (e) {
        if (live) setErr(e instanceof ApiError ? e.message : String(e));
      }
    };
    void go();
    const id = window.setInterval(() => !document.hidden && void go(), 4000);
    return () => {
      live = false;
      window.clearInterval(id);
    };
  }, [name]);

  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines]);

  return (
    <>
      {err ? (
        <EmptyState icon="alert" hue="svc" title={t('logs.error')} text={err} />
      ) : !lines ? (
        <Skeleton lines={6} height={16} />
      ) : lines.length === 0 ? (
        <EmptyState icon="logs" hue="svc" title={t('logs.empty')} text={t('logs.emptyHint')} />
      ) : (
        <pre className="svc-logs" ref={box} tabIndex={0} aria-label={t('panel.logs')}>
          {lines.map((l, i) => (
            <div key={i} className={l.priority <= 3 ? 'svc-l-err' : l.priority === 4 ? 'svc-l-warn' : undefined}>
              <span className="svc-l-t">{formatClockTime(l.time)}</span> {l.message}
            </div>
          ))}
        </pre>
      )}
      <button type="button" className="svc-lnk" onClick={() => nav(`/logs?unit=${encodeURIComponent(name)}`)}>
        <Icon name="externallink" />
        {t('openInLogs')}
      </button>
    </>
  );
}

function UnitFileTab({ name, onSaved }: { name: string; onSaved(): void }) {
  const t = useT('services');
  const [file, setFile] = useState<UnitFile | null>(null);
  const [err, setErr] = useState('');
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState(false);

  const load = useCallback(async () => {
    try {
      setFile(await call<UnitFile>('services.unitFile', { name }));
      setErr('');
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    }
  }, [name]);
  useEffect(() => {
    void load();
  }, [load]);

  const save = async (content: string) => {
    setSaving(true);
    try {
      await call('services.saveOverride', { name, content });
      toast.ok(content.trim() ? t('unit.saved', { name: short(name) }) : t('unit.removed', { name: short(name) }));
      setEditing(false);
      await load();
      onSaved();
    } catch (e) {
      toast.err(t('unit.saveFailed'), e instanceof ApiError ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  if (err) return <EmptyState icon="alert" hue="svc" title={t('unit.error')} text={err} />;
  if (!file) return <Skeleton lines={8} height={16} />;

  if (editing) {
    return (
      <>
        <p className="svc-note">{t('unit.editNote', { path: file.overridePath })}</p>
        <Textarea mono className="svc-editor" value={text} onChange={(e) => setText(e.target.value)} rows={14} spellCheck={false} aria-label={t('unit.editorLabel')} />
        <p className="svc-note">{t('unit.applyNote')}</p>
        <div className="svc-pacts">
          <Button variant="primary" loading={saving} onClick={() => void save(text)}>{t('save')}</Button>
          <Button disabled={saving} onClick={() => setEditing(false)}>{t('cancel')}</Button>
        </div>
      </>
    );
  }

  const extra = file.overrides;
  return (
    <>
      <h4 className="svc-h4">{t('unit.package')}</h4>
      <div className="svc-path svc-mono">{file.path || t('unit.noFile')}</div>
      {file.content && <pre className="svc-unit">{file.content}</pre>}
      <h4 className="svc-h4">{t('unit.overrides')}</h4>
      {extra.length === 0 ? (
        <p className="svc-note">{t('unit.noOverrides')}</p>
      ) : (
        extra.map((o) => (
          <div key={o.path}>
            <div className="svc-path svc-mono">{o.path}</div>
            <pre className="svc-unit svc-unit--ov">{o.content}</pre>
          </div>
        ))
      )}
      <div className="svc-pacts">
        <Button icon="edit" variant="primary" onClick={() => { setText(file.override || '[Service]\n'); setEditing(true); }}>{t('unit.edit')}</Button>
        {file.override.trim() && <Button variant="ghost" onClick={() => setRemoving(true)}>{t('unit.remove')}</Button>}
      </div>
      <p className="svc-note">{t('unit.safeNote')}</p>
      <ConfirmDialog
        open={removing}
        onClose={() => setRemoving(false)}
        onConfirm={() => save('')}
        title={t('unit.removeTitle', { name: short(name) })}
        description={t('unit.removeText')}
        confirmLabel={t('unit.remove')}
        danger={false}
      />
    </>
  );
}

function DepsTab({ detail, onOpenUnit }: { detail: Detail | null; onOpenUnit(n: string): void }) {
  const t = useT('services');
  if (!detail) return <Skeleton lines={6} height={16} />;
  const d = detail.dependencies;
  const uniq = (...a: string[][]) => [...new Set(a.flat())];
  const groups: { id: string; items: string[] }[] = [
    { id: 'needs', items: uniq(d.requires ?? [], d.wants ?? []) },
    { id: 'neededBy', items: uniq(d.requiredBy ?? [], d.wantedBy ?? []) },
    { id: 'conflicts', items: d.conflicts ?? [] },
    { id: 'triggers', items: d.triggers ?? [] },
    { id: 'triggeredBy', items: d.triggeredBy ?? [] },
  ];
  const shown = groups.filter((g) => g.items.length || ['needs', 'neededBy', 'conflicts'].includes(g.id));
  return (
    <>
      {shown.map((g) => (
        <section key={g.id} className="svc-dep">
          <h4 className="svc-h4">{t(`deps.${g.id}`)}</h4>
          {g.items.length === 0 ? (
            <span className="svc-muted">{t('deps.none')}</span>
          ) : (
            <div className="svc-chips-list">
              {g.items.map((n) => (
                <button key={n} type="button" className="svc-depchip" onClick={() => onOpenUnit(n)}>{n}</button>
              ))}
            </div>
          )}
        </section>
      ))}
    </>
  );
}

