import { useMemo, useState, type FormEvent } from 'react';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { Button, Dialog, Icon, Input, Segmented, Select, Switch, hueClass, type HueId } from '../../ui';
import { useMetrics } from './data';
import { ACTION_ICONS, asMetric, asRange, HUES, isAction, joinArgv, METRICS, splitArgv, uid, type DashAction, type Metric, type Range, type Widget } from './model';

interface ActionDraft {
  id: string;
  label: string;
  icon: string;
  hue: HueId;
  cmd: string;
  admin: boolean;
  confirm: boolean;
}

const toDraft = (a?: Partial<DashAction>): ActionDraft => ({
  id: a?.id || uid('a'),
  label: a?.label ?? '',
  icon: a?.icon ?? 'play',
  hue: a?.hue ?? 'sw',
  cmd: a?.argv ? joinArgv(a.argv) : '',
  admin: !!a?.admin,
  confirm: !!a?.confirm,
});

type T = ReturnType<typeof useT>;

function parseCmd(cmd: string, t: T): { argv?: string[]; error?: string } {
  const argv = splitArgv(cmd.trim());
  if (!argv) return { error: t('settings.errQuote') };
  if (!argv.length) return { error: t('settings.errCommand') };
  return { argv };
}

function draftToAction(d: ActionDraft, t: T): { action?: DashAction; error?: string } {
  if (!d.label.trim()) return { error: t('settings.errLabel') };
  const p = parseCmd(d.cmd, t);
  if (!p.argv) return { error: p.error };
  return { action: { id: d.id, label: d.label.trim(), icon: d.icon, hue: d.hue, argv: p.argv, admin: d.admin || undefined, confirm: d.confirm || undefined } };
}

function ActionFields({ d, onChange, onRemove, compact }: { d: ActionDraft; onChange(d: ActionDraft): void; onRemove?: () => void; compact?: boolean }) {
  const t = useT('overview');
  const win = useSession().session?.os === 'windows';
  const th = useT('shell');
  const set = (p: Partial<ActionDraft>) => onChange({ ...d, ...p });
  return (
    <div className={`ov-af${compact ? ' ov-af--compact' : ''}`}>
      <div className="ov-af-row">
        <Input label={t('settings.label')} value={d.label} onChange={(e) => set({ label: e.target.value })} placeholder={t('settings.labelPh')} maxLength={40} />
        {onRemove && <Button variant="ghost" iconOnly icon="trash" aria-label={t('settings.removeAction')} onClick={onRemove} className="ov-af-del" />}
      </div>
      <Input label={t('settings.command')} mono value={d.cmd} onChange={(e) => set({ cmd: e.target.value })} placeholder={win ? "Restart-Service Spooler" : "systemctl restart nginx"} hint={win ? t('settings.commandHintWin') : t('settings.commandHint')} spellCheck={false} autoComplete="off" />
      <div>
        <div className="ui-label">{t('settings.colour')}</div>
        <div className="ov-swatches" role="radiogroup" aria-label={t('settings.colour')}>
          {HUES.map((h) => (
            <button key={h} type="button" role="radio" aria-checked={d.hue === h} aria-label={th(`nav.${hueNav[h]}`)} className={`ov-swatch ${hueClass(h)}`} onClick={() => set({ hue: h })}>
              {d.hue === h && <Icon name="check" />}
            </button>
          ))}
        </div>
      </div>
      <div>
        <div className="ui-label">{t('settings.icon')}</div>
        <div className="ov-icons" role="radiogroup" aria-label={t('settings.icon')}>
          {ACTION_ICONS.map((ic) => (
            <button key={ic} type="button" role="radio" aria-checked={d.icon === ic} aria-label={ic} className={`ov-icbtn ${hueClass(d.hue)}`} onClick={() => set({ icon: ic })}>
              <Icon name={ic} />
            </button>
          ))}
        </div>
      </div>
      <Switch checked={d.admin} onChange={(admin) => set({ admin })} label={t('settings.admin')} />
      <Switch checked={d.confirm} onChange={(confirm) => set({ confirm })} label={t('settings.confirm')} />
    </div>
  );
}
const hueNav: Record<HueId, string> = { ov: 'overview', term: 'terminal', file: 'files', log: 'logs', svc: 'services', sw: 'software', usr: 'users', plg: 'plugins' };

const WIN_DESC = new Set(['service', 'log']);

export function SettingsDialog({ widget, onClose, onSave, pluginTitle }: { widget: Widget | null; onClose(): void; onSave(settings: Record<string, any>): void; pluginTitle?: string }) {
  if (!widget) return null;
  return <Inner key={widget.id} widget={widget} onClose={onClose} onSave={onSave} pluginTitle={pluginTitle} />;
}

function Inner({ widget, onClose, onSave, pluginTitle }: { widget: Widget; onClose(): void; onSave(s: Record<string, any>): void; pluginTitle?: string }) {
  const t = useT('overview');
  const win = useSession().session?.os === 'windows';
  const { metrics } = useMetrics();
  const s = widget.settings ?? {};
  const [title, setTitle] = useState<string>(s.title ?? '');
  const [metric, setMetric] = useState(asMetric(s.metric));
  const [range, setRange] = useState(asRange(s.range, widget.type === 'chart' ? '15m' : '1h'));
  const [mount, setMount] = useState<string>(s.mount ?? '');
  const [iface, setIface] = useState<string>(s.iface ?? 'auto');
  const [showOk, setShowOk] = useState(s.showOk !== false);
  const [max, setMax] = useState(String(s.max ?? 6));
  const [units, setUnits] = useState<string>(Array.isArray(s.units) ? s.units.join(', ') : '');
  const [source, setSource] = useState<'journal' | 'unit' | 'file' | 'evt'>(s.source === 'unit' || s.source === 'file' ? s.source : typeof s.source === 'string' && s.source.startsWith('evt:') ? 'evt' : win ? 'evt' : 'journal');
  const [channel, setChannel] = useState<string>(typeof s.source === 'string' && s.source.startsWith('evt:') ? s.source.slice(4) : 'System');
  const [unit, setUnit] = useState<string>(s.unit ?? '');
  const [file, setFile] = useState<string>(s.file ?? '');
  const [lines, setLines] = useState(String(s.lines ?? 12));
  const [cmd, setCmd] = useState(Array.isArray(s.argv) ? joinArgv(s.argv) : '');
  const [every, setEvery] = useState(String(s.every ?? 10));
  const [admin, setAdmin] = useState(!!s.admin);
  const [path, setPath] = useState<string>(s.path ?? '');
  const [label, setLabel] = useState<string>(s.label ?? '');
  const [one, setOne] = useState<ActionDraft>(() => toDraft(isAction(s) ? (s as DashAction) : undefined));
  const [many, setMany] = useState<ActionDraft[]>(() => (Array.isArray(s.actions) ? (s.actions as unknown[]).filter(isAction).map((a) => toDraft(a)) : []));
  const [error, setError] = useState('');

  const type = widget.type;
  const mounts = useMemo(() => (metrics?.disks ?? []).map((d) => ({ value: d.mount, label: d.mount })), [metrics]);
  const ifaces = useMemo(() => (metrics?.net ?? []).map((n) => ({ value: n.iface, label: n.iface })), [metrics]);

  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    const base: Record<string, any> = {};
    const tt = title.trim();
    if (tt && type !== 'action' && type !== 'folder') base.title = tt;
    switch (type) {
      case 'stat':
        Object.assign(base, { metric });
        if (metric === 'disk' && mount) base.mount = mount;
        if (metric === 'network' && iface !== 'auto') base.iface = iface;
        break;
      case 'activity':
        Object.assign(base, { range });
        break;
      case 'chart':
        Object.assign(base, { metric, range });
        if (metric === 'disk' && mount) base.mount = mount;
        break;
      case 'alerts': {
        const n = Math.max(3, Math.min(12, parseInt(max, 10) || 6));
        Object.assign(base, { showOk, max: n });
        break;
      }
      case 'service': {
        const list = units.split(/[\s,]+/).map((u) => u.trim()).filter(Boolean);
        if (list.length > 30) return setError(t('settings.errUnits'));
        Object.assign(base, { units: [...new Set(list)] });
        break;
      }
      case 'log':
        if (source === 'unit' && !unit.trim()) return setError(t('settings.errUnit'));
        if (source === 'evt' && !channel.trim()) return setError(t('settings.errChannel'));
        if (source === 'file' && !(win ? /^([a-zA-Z]:[\\/]|\\\\)/.test(file.trim()) : file.trim().startsWith('/'))) return setError(t(win ? 'win.settings.errFile' : 'settings.errFile'));
        Object.assign(base, { source: source === 'evt' ? `evt:${channel.trim()}` : source, lines: parseInt(lines, 10) || 12 });
        if (source === 'unit') base.unit = unit.trim();
        if (source === 'file') base.file = file.trim();
        break;
      case 'output': {
        const p = parseCmd(cmd, t);
        if (!p.argv) return setError(p.error!);
        Object.assign(base, { argv: p.argv, every: Math.max(2, parseInt(every, 10) || 10), admin: admin || undefined });
        break;
      }
      case 'folder':
        if (!(win ? /^([a-zA-Z]:[\\/]|\\\\)/.test(path.trim()) : path.trim().startsWith('/'))) return setError(t(win ? 'win.settings.errPath' : 'settings.errPath'));
        Object.assign(base, { path: path.trim(), label: label.trim() });
        break;
      case 'action': {
        const r = draftToAction(one, t);
        if (!r.action) return setError(r.error!);
        return onSave(r.action as unknown as Record<string, any>);
      }
      case 'actions': {
        const out: DashAction[] = [];
        for (const d of many) {
          const r = draftToAction(d, t);
          if (!r.action) return setError(`${d.label.trim() || t('settings.untitled')}: ${r.error}`);
          out.push(r.action);
        }
        Object.assign(base, { actions: out });
        break;
      }
      case 'plugin':
        Object.assign(base, { plugin: s.plugin, widget: s.widget });
        break;
      default:
        Object.assign(base, s, tt ? { title: tt } : {});
    }
    onSave(base);
  };

  const hasTitle = type !== 'action' && type !== 'folder';
  const noExtra = type === 'machine' || type === 'plugin';
  return (
    <Dialog
      open
      onClose={onClose}
      size="lg"
      icon="cog"
      title={t('settings.title', { name: t(`widgets.${type}.title`) })}
      description={(() => { const k = win && WIN_DESC.has(type) ? `win.widgets.${type}.desc` : `widgets.${type}.desc`; return t(k) === k ? undefined : t(k); })()}
      onSubmit={submit}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t('cancel')}</Button>
          <Button type="submit" variant="primary">{t('settings.apply')}</Button>
        </>
      }
    >
      <div className="ov-form">
        {hasTitle && <Input label={t('settings.widgetTitle')} value={title} onChange={(e) => setTitle(e.target.value)} placeholder={type === 'plugin' ? pluginTitle : t(`widgets.${type}.title`)} maxLength={60} />}

        {(type === 'stat' || type === 'chart') && (
          <div>
            <div className="ui-label">{t('settings.metric')}</div>
            <Segmented<Metric> value={metric} onChange={setMetric} aria-label={t('settings.metric')} options={METRICS.map((m) => ({ value: m, label: t(`metrics.${m}`) }))} />
          </div>
        )}
        {(type === 'stat' || type === 'chart') && metric === 'disk' && (
          <Select label={t('settings.mount')} value={mount} onChange={setMount} options={[{ value: '', label: t('settings.auto') }, ...mounts]} />
        )}
        {type === 'stat' && metric === 'network' && (
          <Select label={t('settings.iface')} value={iface} onChange={setIface} options={[{ value: 'auto', label: t('settings.auto') }, ...ifaces]} />
        )}
        {(type === 'activity' || type === 'chart') && (
          <div>
            <div className="ui-label">{t('settings.range')}</div>
            <Segmented<Range> value={range} onChange={setRange} aria-label={t('settings.range')} options={(['5m', '15m', '1h'] as const).map((r) => ({ value: r, label: t(`range.${r}`) }))} />
          </div>
        )}

        {type === 'alerts' && (
          <>
            <Switch checked={showOk} onChange={setShowOk} label={t('settings.showOk')} />
            <Select label={t('settings.max')} value={max} onChange={setMax} options={[3, 4, 5, 6, 8, 10, 12].map((n) => ({ value: String(n), label: String(n) }))} />
          </>
        )}

        {type === 'service' && <Input label={t('settings.units')} mono value={units} onChange={(e) => setUnits(e.target.value)} placeholder={win ? "Spooler, W32Time" : "nginx, sshd, docker"} hint={t('settings.unitsHint')} spellCheck={false} />}

        {type === 'log' && (
          <>
            <div>
              <div className="ui-label">{t('settings.source')}</div>
              <Segmented<'journal' | 'unit' | 'file' | 'evt'> value={source} onChange={setSource} aria-label={t('settings.source')} options={win ? [{ value: 'evt', label: t('settings.srcEvt') }, { value: 'file', label: t('settings.srcFile') }] : [{ value: 'journal', label: t('settings.srcJournal') }, { value: 'unit', label: t('settings.srcUnit') }, { value: 'file', label: t('settings.srcFile') }]} />
            </div>
            {source === 'evt' && <Input label={t('settings.channel')} mono value={channel} onChange={(e) => setChannel(e.target.value)} placeholder="System" spellCheck={false} />}
            {source === 'unit' && <Input label={t('settings.unit')} mono value={unit} onChange={(e) => setUnit(e.target.value)} placeholder="nginx" spellCheck={false} />}
            {source === 'file' && <Input label={t('settings.file')} mono value={file} onChange={(e) => setFile(e.target.value)} placeholder={win ? 'C:\\Windows\\Logs\\CBS\\CBS.log' : '/var/log/pacman.log'} hint={t('settings.fileHint')} spellCheck={false} />}
            <Select label={t('settings.lines')} value={lines} onChange={setLines} options={[6, 8, 12, 20, 40].map((n) => ({ value: String(n), label: String(n) }))} />
          </>
        )}

        {type === 'output' && (
          <>
            <Input label={t('settings.command')} mono value={cmd} onChange={(e) => setCmd(e.target.value)} placeholder="uptime" hint={win ? t('settings.commandHintWin') : t('settings.commandHint')} spellCheck={false} />
            <Select label={t('settings.every')} value={every} onChange={setEvery} options={[2, 5, 10, 30, 60, 300].map((n) => ({ value: String(n), label: t('settings.seconds', { count: n }) }))} />
            <Switch checked={admin} onChange={setAdmin} label={t('settings.admin')} />
          </>
        )}

        {type === 'folder' && (
          <>
            <Input label={t('settings.path')} mono value={path} onChange={(e) => setPath(e.target.value)} placeholder="/home/you/Documents" spellCheck={false} />
            <Input label={t('settings.folderLabel')} value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t('settings.folderLabelPh')} maxLength={40} />
          </>
        )}

        {type === 'action' && <ActionFields d={one} onChange={setOne} />}

        {type === 'actions' && (
          <div className="ov-af-list">
            {many.map((d, i) => (
              <ActionFields key={d.id} d={d} compact onChange={(x) => setMany(many.map((y, j) => (j === i ? x : y)))} onRemove={() => setMany(many.filter((_, j) => j !== i))} />
            ))}
            <Button icon="plus" onClick={() => setMany([...many, toDraft()])} disabled={many.length >= 12}>{t('settings.addAction')}</Button>
          </div>
        )}

        {noExtra && !hasTitle && <small className="ov-muted">{t('settings.none')}</small>}
        {error && <div className="ui-form-err" role="alert"><Icon name="alert" />{error}</div>}
      </div>
    </Dialog>
  );
}
