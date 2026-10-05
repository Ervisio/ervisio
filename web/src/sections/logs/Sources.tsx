import { useState } from 'react';
import { Button, Checkbox, Chip, EmptyState, Icon, IconButton, Input, Select, Skeleton, type IconName } from '../../ui';
import { call, useSession } from '../../api';
import { useT } from '../../i18n';
import { hueFor } from './helpers';
import type { Source, SourcesResult, Watcher, WatchFormat } from './types';

interface Props {
  data: SourcesResult | null;
  error?: string;
  selected: string[];
  watchers: Watcher[];
  onSelect(ids: string[]): void;
  onAddWatcher(w: Watcher): void;
  onRemoveWatcher(path: string): void;
  onUnlock(): void;
  onRetry(): void;
  canUnlock: boolean;
}

const KIND_ICON: Record<string, IconName> = { journal: 'logs', kernel: 'cpu', unit: 'services', file: 'file', evt: 'logs' };

function count(n: number, approx = false): string {
  const s = n >= 10000 ? `${Math.round(n / 1000)}k` : n.toLocaleString();
  return approx ? `${s}+` : s;
}

function SourceItem({ s, on, onPick, onRemove }: { s: Source; on: boolean; onPick(multi: boolean): void; onRemove?: () => void }) {
  const t = useT('logs');
  const hue = s.kind === 'journal' ? 'ov' : s.kind === 'kernel' ? 'svc' : s.kind === 'evt' ? 'ov' : s.group === 'watchers' ? 'log' : s.group === 'files' ? 'sw' : hueFor(s.label);
  const label = s.id === 'journal' ? t('sources.journal') : s.id === 'kernel' ? t('sources.kernel') : s.id === 'boot' ? t('sources.boot') : s.label;
  const hint = s.id === 'journal' ? t('sources.journalHint') : s.id === 'kernel' ? t('sources.kernelHint') : s.id === 'boot' ? t('sources.bootHint') : s.hint;
  return (
    <div className={`logs-si logs-h-${hue}${on ? ' on' : ''}`}>
      <button type="button" className="logs-si-main" aria-pressed={on} title={t('sources.multiHint')} onClick={(e) => onPick(e.ctrlKey || e.metaKey)}>
        <span className="logs-tile"><Icon name={KIND_ICON[s.kind] ?? 'logs'} /></span>
        <span className="logs-si-tx">
          <b>{label}</b>
          <small>{hint}</small>
        </span>
        {s.needsAdmin ? (
          <span className="logs-si-lock" title={t('sources.needsAdmin')}><Icon name="lock" /></span>
        ) : s.id !== 'boot' ? (
          <span className={`logs-ct${s.errors24h > 0 ? ' err' : ''}`} title={s.errors24h > 0 ? t('sources.errors', { count: s.errors24h }) : undefined}>{count(s.count24h)}</span>
        ) : null}
      </button>
      {onRemove && (
        <span className="logs-si-x">
          <IconButton icon="close" label={t('sources.remove', { name: label })} size="sm" onClick={onRemove} />
        </span>
      )}
    </div>
  );
}

function WatchForm({ watchers, onAdd, onClose }: { watchers: Watcher[]; onAdd(w: Watcher): void; onClose(): void }) {
  const t = useT('logs');
  const [path, setPath] = useState('');
  const [format, setFormat] = useState<WatchFormat>('auto');
  const [notify, setNotify] = useState(true);
  const [keep, setKeep] = useState('7');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const p = path.trim();
    if (!p.startsWith('/')) return setError(t('watch.errAbsolute'));
    if (watchers.some((w) => w.path === p)) return setError(t('watch.errExists'));
    setBusy(true);
    setError('');
    try {
      await call('logs.query', { sources: [`file:${p}`], limit: 1, watchers: [] }, { noUnlock: true });
    } catch (e: any) {
      // A file only root can read is fine: it is listed and asks for the unlock when opened.
      if (e?.code === 'not_found') {
        setBusy(false);
        return setError(t('watch.errNotFound'));
      }
      if (e?.code === 'invalid') {
        setBusy(false);
        return setError(t('watch.errNotFile'));
      }
    }
    setBusy(false);
    onAdd({ path: p, name: p.slice(p.lastIndexOf('/') + 1), format, notify, keepDays: Number(keep) });
  };

  return (
    <form
      className="logs-watch"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <h4>{t('watch.title')}</h4>
      <Input
        label={t('watch.path')}
        icon="files"
        mono
        compact
        autoFocus
        value={path}
        onChange={(e) => {
          setPath(e.target.value);
          setError('');
        }}
        placeholder={t('watch.pathPlaceholder')}
        error={error || undefined}
        spellCheck={false}
      />
      <div className="logs-watch-row">
        <span className="logs-lbl">{t('watch.format')}</span>
        <div className="logs-chips" role="radiogroup" aria-label={t('watch.format')}>
          {(['plain', 'auto', 'json'] as WatchFormat[]).map((f) => (
            <Chip key={f} pressed={format === f} onClick={() => setFormat(f)}>
              {t(`watch.${f}`)}
            </Chip>
          ))}
        </div>
      </div>
      <Checkbox checked={notify} onChange={setNotify} label={t('watch.notify')} />
      <Select
        compact
        label={t('watch.keep')}
        value={keep}
        onChange={setKeep}
        options={[1, 3, 7, 14, 30].map((n) => ({ value: String(n), label: t('watch.keepDays', { count: n }) }))}
      />
      <div className="logs-watch-act">
        <Button type="button" variant="ghost" onClick={onClose}>{t('watch.cancel')}</Button>
        <Button type="submit" variant="primary" loading={busy}>{t('watch.start')}</Button>
      </div>
    </form>
  );
}

export function Sources({ data, error, selected, watchers, onSelect, onAddWatcher, onRemoveWatcher, onUnlock, onRetry, canUnlock }: Props) {
  const t = useT('logs');
  const win = useSession().session?.os === 'windows';
  const [adding, setAdding] = useState(false);
  const allOn = selected.length === 1 && selected[0] === 'all';

  const pick = (id: string, multi: boolean) => {
    if (multi && !allOn) {
      onSelect(selected.includes(id) ? (selected.length > 1 ? selected.filter((s) => s !== id) : ['all']) : [...selected, id]);
    } else if (!multi && selected.length === 1 && selected[0] === id) onSelect(['all']);
    else onSelect([id]);
  };

  const groupTitle = (id: string) => t(`sources.${id}`);

  return (
    <aside className="logs-srcs" aria-label={t('sources.title')}>
      <div className="logs-srcs-list">
        <button type="button" className={`logs-all${allOn ? ' on' : ''}`} aria-pressed={allOn} onClick={() => onSelect(['all'])}>
          <Icon name="logs" />
          <span>{t('sources.all')}</span>
          {data && <span className="ct">{t(data.approx ? 'sources.approx' : 'sources.allCount', { count: count(data.total24h) })}</span>}
        </button>

        {!data && !error && (
          <div className="logs-srcs-skel"><Skeleton lines={6} height={34} /></div>
        )}
        {error && !data && (
          <EmptyState icon="alert" title={t('sources.loadError')} text={error} action={<Button onClick={onRetry}>{t('stream.retry')}</Button>} />
        )}
        {data && !data.journalReadable && (
          <div className="logs-lockbox">
            <b>{t('sources.unlockTitle')}</b>
            <small>{t(win ? 'win.sources.unlockText' : 'sources.unlockText')}</small>
            {canUnlock && <Button size="sm" variant="primary" icon="unlock" onClick={onUnlock}>{t('sources.unlock')}</Button>}
          </div>
        )}
        {data?.groups.map((g) => {
          const list = g.id === 'watchers' ? g.sources : g.sources;
          if (g.id !== 'system' && g.id !== 'watchers' && !list.length) return null;
          return (
            <section key={g.id} aria-label={groupTitle(g.id)}>
              <div className="logs-grp">{groupTitle(g.id)}</div>
              {list.map((s) => (
                <SourceItem
                  key={s.id}
                  s={s}
                  on={!allOn && selected.includes(s.id)}
                  onPick={(m) => pick(s.id, m)}
                  onRemove={g.id === 'watchers' ? () => onRemoveWatcher(s.path ?? '') : undefined}
                />
              ))}
              {g.id === 'watchers' && !list.length && <div className="logs-none">{t('sources.noWatchers')}</div>}
            </section>
          );
        })}
        {data && <div className="logs-pad" />}
      </div>
      <div className="logs-srcs-foot">
        {adding ? (
          <WatchForm
            watchers={watchers}
            onClose={() => setAdding(false)}
            onAdd={(w) => {
              onAddWatcher(w);
              setAdding(false);
            }}
          />
        ) : (
          <button type="button" className="logs-addw" onClick={() => setAdding(true)}>
            <Icon name="plus" />
            {t('watch.button')}
          </button>
        )}
      </div>
    </aside>
  );
}

