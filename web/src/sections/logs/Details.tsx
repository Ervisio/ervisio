import { useEffect, useState } from 'react';
import { Badge, Icon, Panel, Skeleton, toast, type Tone } from '../../ui';
import { useI18n, useT } from '../../i18n';
import { entryText, fmtClock, fmtFull, hueFor } from './helpers';
import type { ContextResult, Entry, Watcher } from './types';

interface Props {
  entry: Entry | null;
  /** Source id to restrict journal context to (a selected unit), else the entry's own. */
  contextSource: string;
  watchers: Watcher[];
  call_: <T>(method: string, params: unknown, signal?: AbortSignal) => Promise<T>;
  onClose(): void;
  onOpenFile(path: string): void;
  onGoService(unit: string): void;
  onOnlySource(id: string): void;
  inline: boolean;
}

const TONE: Record<string, Tone> = { err: 'err', warn: 'warn', info: 'info', debug: 'neutral' };

function Context({ entry, contextSource, watchers, call_ }: Pick<Props, 'entry' | 'contextSource' | 'watchers' | 'call_'>) {
  const t = useT('logs');
  const [state, setState] = useState<{ data?: ContextResult; error?: string; loading: boolean }>({ loading: true });
  const key = entry?.key;
  useEffect(() => {
    if (!entry) return;
    const usable = entry.file ? !!entry.line : true;
    if (!usable) {
      setState({ loading: false });
      return;
    }
    const ac = new AbortController();
    setState({ loading: true });
    call_<ContextResult>('logs.context', { source: contextSource, cursor: entry.cursor, before: 4, after: 4, watchers }, ac.signal)
      .then((data) => !ac.signal.aborted && setState({ data, loading: false }))
      .catch((e) => !ac.signal.aborted && setState({ error: e?.message ?? String(e), loading: false }));
    return () => ac.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  if (state.loading) return <Skeleton lines={4} height={14} />;
  if (state.error) return <p className="logs-ctx-msg">{t('details.contextError')}</p>;
  const d = state.data;
  if (!d || !d.entries.length) return <p className="logs-ctx-msg">{t('details.contextEmpty')}</p>;
  return (
    <pre className="logs-ctx" tabIndex={0}>
      {d.entries.map((e, i) => (
        <div key={i} className={i === d.index ? `cur lv-${e.level}` : 'd'}>
          <span className="t">{fmtClock(e.ts, false)}</span> {e.message}
        </div>
      ))}
    </pre>
  );
}

export function Details({ entry, contextSource, watchers, call_, onClose, onOpenFile, onGoService, onOnlySource, inline }: Props) {
  const t = useT('logs');
  const { lang } = useI18n();

  const copy = async () => {
    if (!entry) return;
    try {
      await navigator.clipboard.writeText(entryText(entry));
      toast.ok(t('details.copied'));
    } catch {
      toast.err(t('details.copyFailed'));
    }
  };

  const unit = entry?.unit && entry.unit.endsWith('.service') ? entry.unit : '';
  const goName = unit.replace(/\.service$/, '');
  const onlyId = entry ? (entry.file ? `file:${entry.file}` : unit ? `unit:${unit}` : '') : '';

  return (
    <Panel
      open={!!entry}
      inline={inline}
      onClose={onClose}
      title={entry?.source ?? ''}
      subtitle={entry ? fmtFull(entry.ts, lang) : undefined}
      icon="logs"
      hue="log"
    >
      {entry && (
        <div className="logs-det">
          <div className="logs-det-hd">
            <Badge tone={TONE[entry.level]}>{t(`level.${entry.level}`)}</Badge>
            <span className={`logs-det-src logs-h-${hueFor(entry.source)}`}>{entry.source}</span>
          </div>
          <h3 className="logs-det-msg">{entry.message}</h3>
          <dl className="logs-kv">
            <dt>{t('details.time')}</dt>
            <dd>{fmtFull(entry.ts, lang)}</dd>
            {entry.unit && (<><dt>{t('details.unit')}</dt><dd className="mono">{entry.unit}</dd></>)}
            {!!entry.pid && (<><dt>{t('details.pid')}</dt><dd className="mono">{entry.pid}</dd></>)}
            {entry.file && (<><dt>{t('details.file')}</dt><dd className="mono" title={entry.file}>{entry.file}</dd></>)}
            {!!entry.line && (<><dt>{t('details.line')}</dt><dd>{entry.line.toLocaleString()}</dd></>)}
          </dl>
          {(entry.file || !contextSource.startsWith('evt:')) && (
            <>
              <h4>{t('details.context')}</h4>
              <Context entry={entry} contextSource={contextSource} watchers={watchers} call_={call_} />
            </>
          )}
          <div className="logs-acts">
            {entry.file && (
              <button type="button" onClick={() => onOpenFile(entry.file!)}>
                <Icon name="files" />
                {t('details.openFile')}
              </button>
            )}
            <button type="button" onClick={copy}>
              <Icon name="copy" />
              {t('details.copy')}
            </button>
            {unit && (
              <button type="button" onClick={() => onGoService(unit)}>
                <Icon name="services" />
                {t('details.goService', { name: goName })}
              </button>
            )}
            {onlyId && (
              <button type="button" onClick={() => onOnlySource(onlyId)}>
                <Icon name="filter" />
                {t('details.onlySource')}
              </button>
            )}
          </div>
        </div>
      )}
    </Panel>
  );
}

