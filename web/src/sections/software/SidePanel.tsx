import { useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, call } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, Icon, Panel, Progress, Skeleton } from '../../ui';
import { formatBytes } from '../../lib/format';
import { AppIcon } from './AppIcon';
import { SourceBadge, displayName } from './helpers';
import { closePanel, dismissTx, type TxState } from './tx';
import type { Detail, Kind, Update } from './types';

/* ---------------- Transaction panel (live) ---------------- */

function logClass(line: string): string {
  if (line.startsWith('$ ')) return 'sw-l-cmd';
  if (line.startsWith('::')) return 'sw-l-sec';
  if (/^\(\d+\/\d+\)/.test(line)) return 'sw-l-step';
  if (/^(error|e:)/i.test(line) || /\bfailed\b/i.test(line)) return 'sw-l-err';
  if (/^warning/i.test(line)) return 'sw-l-warn';
  return '';
}

export function TxPanel({ tx, openTerminal }: { tx: TxState; openTerminal(): void }) {
  const t = useT('software');
  const ref = useRef<HTMLPreElement>(null);
  const stick = useRef(true);
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    if (tx.phase !== 'running') return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [tx.phase]);

  useEffect(() => {
    const el = ref.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [tx.log]);

  const eta = useMemo(() => {
    if (tx.phase !== 'running' || tx.steps !== 1 || tx.total === 0 || tx.done === 0) return '';
    const elapsed = (now - tx.startedAt) / 1000;
    const left = Math.max(1, Math.round((elapsed / tx.done) * (tx.total - tx.done)));
    return left >= 90 ? t('tx.minutesLeft', { count: Math.round(left / 60) }) : t('tx.secondsLeft', { count: left });
  }, [tx, now, t]);

  const title = tx.title ? t(`tx.title.${tx.title.key}`, tx.title.vars) : t('tx.working');
  const hint = tx.hint ? t(`tx.hint.${tx.hint}`) : '';

  return (
    <Panel
      open={tx.panelOpen && tx.phase !== 'idle'}
      onClose={closePanel}
      title={tx.phase === 'done' ? t(`tx.done.${tx.title?.key ?? 'updateAll'}`, tx.title?.vars) : tx.phase === 'failed' ? t('tx.failed') : title}
      subtitle={tx.attached ? t('tx.attached') : tx.phase === 'running' && tx.steps > 1 ? t('tx.step', { step: tx.step, steps: tx.steps }) : undefined}
      icon="software"
      hue="sw"
      footer={
        <div className="sw-tx-ft">
          <Button icon="terminal" onClick={openTerminal}>{t('openTerminal')}</Button>
          {tx.phase === 'running' ? (
            <Button onClick={closePanel}>{t('tx.hide')}</Button>
          ) : (
            <Button variant="primary" onClick={dismissTx}>{t('tx.close')}</Button>
          )}
        </div>
      }
    >
      <div className="sw-tx">
        <div className="sw-tx-hd">
          <Progress value={tx.phase === 'done' ? 100 : tx.phase === 'failed' ? undefined : tx.total > 0 ? tx.percent : undefined} tone={tx.phase === 'failed' ? 'err' : tx.phase === 'done' ? 'ok' : undefined} hue="sw" label={title} />
          <div className="sw-tx-row">
            <span>{tx.total > 0 ? t('tx.counter', { done: Math.min(tx.done + (tx.phase === 'running' ? 1 : 0), tx.total) || tx.total, total: tx.total }) : tx.phase === 'running' ? t('tx.preparing') : ''}</span>
            <span className="sw-muted">{eta}</span>
          </div>
          {tx.phase === 'running' && tx.current && <div className="sw-tx-cur sw-mono">{tx.current}</div>}
        </div>
        <pre
          ref={ref}
          className="sw-log"
          tabIndex={0}
          aria-label={t('tx.log')}
          onScroll={(e) => {
            const el = e.currentTarget;
            stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
          }}
        >
          {tx.log.map((l, i) => (
            <span key={i} className={logClass(l)}>{l}{'\n'}</span>
          ))}
        </pre>
        {tx.phase === 'failed' && (
          <div className="sw-notice sw-notice--err">
            <Icon name="alert" />
            <div>
              <b>{t('tx.failed')}</b>
              <div>{tx.message}</div>
              {hint && <small>{hint}</small>}
            </div>
          </div>
        )}
        {tx.phase === 'done' && !tx.rebootNeeded && (
          <div className="sw-notice sw-notice--ok"><Icon name="check" /><div>{t('tx.finished')}</div></div>
        )}
        {tx.rebootNeeded && (
          <div className="sw-notice sw-notice--warn">
            <Icon name="alert" />
            <div>{tx.phase === 'running' ? t('tx.rebootLater') : t('tx.rebootNow')}</div>
          </div>
        )}
      </div>
    </Panel>
  );
}

/* ---------------- Package panel ---------------- */

export interface PkgTarget {
  name: string;
  title?: string;
  kind: Kind;
  source: string;
  installed: boolean;
  icon?: string;
  scope?: 'system' | 'user';
}

const SKIP = new Set(['Name']);

export function PkgPanel({
  target, update, busy, onClose, onInstall, onRemove, onUpdate, openTerminal,
}: {
  target: PkgTarget | null;
  update?: Update;
  busy: boolean;
  onClose(): void;
  onInstall(t: PkgTarget, d: Detail | null): void;
  onRemove(t: PkgTarget): void;
  onUpdate(u: Update): void;
  openTerminal(): void;
}) {
  const t = useT('software');
  const [detail, setDetail] = useState<Detail | null>(null);
  const [err, setErr] = useState('');

  useEffect(() => {
    setDetail(null);
    setErr('');
    if (!target) return;
    let live = true;
    call<Detail>('software.info', { name: target.name, source: target.kind === 'repo' ? target.source : target.kind })
      .then((d) => live && setDetail(d))
      .catch((e) => live && setErr(e instanceof ApiError ? e.message : String(e)));
    return () => {
      live = false;
    };
  }, [target]);

  const installed = detail ? detail.installed : target?.installed;
  const label = target ? displayName(target) : '';
  return (
    <Panel
      open={!!target}
      onClose={onClose}
      title={label}
      subtitle={detail?.version || undefined}
      icon="software"
      hue="sw"
      footer={
        target && (
          <div className="sw-tx-ft">
            {update ? (
              update.kind === 'aur' ? (
                <Button icon="terminal" onClick={openTerminal}>{t('terminal')}</Button>
              ) : (
                <Button variant="primary" icon="download" disabled={busy} onClick={() => onUpdate(update)}>{t('update')}</Button>
              )
            ) : null}
            {installed ? (
              <Button variant="danger" icon="trash" disabled={busy} onClick={() => onRemove(target)}>{t('remove.button')}</Button>
            ) : target.kind === 'aur' ? (
              <Button icon="terminal" onClick={openTerminal}>{t('terminal')}</Button>
            ) : (
              <Button variant="primary" icon="download" disabled={busy} onClick={() => onInstall(target, detail)}>{t('install.button')}</Button>
            )}
          </div>
        )
      }
    >
      {target && (
        <div className="sw-pp">
          <div className="sw-pp-hd">
            <AppIcon icon={target.icon} label={label} size={56} />
            <div>
              <b>{label}</b>
              <div className="sw-pp-m">
                <SourceBadge kind={target.kind} source={target.source} />
                {installed ? <Badge tone="ok">{t('installedBadge')}</Badge> : null}
                {update && <Badge tone="info">{t('update.to', { version: update.to })}</Badge>}
              </div>
            </div>
          </div>
          {detail?.description && <p className="sw-pp-d">{detail.description}</p>}
          {err ? (
            <div className="sw-notice sw-notice--err"><Icon name="alert" /><div>{err}</div></div>
          ) : !detail ? (
            <Skeleton lines={6} height={14} />
          ) : (
            <dl className="sw-kv sw-kv--panel">
              {detail.fields
                .filter((f) => !SKIP.has(f.key) && f.key !== 'Description' && f.key !== 'Summary')
                .map((f) => (
                  <div key={f.key} className="sw-kv-r">
                    <dt>{f.key}</dt>
                    <dd className={/size|date|version|id|ref|arch/i.test(f.key) ? 'sw-mono' : undefined}>{prettyValue(f.key, f.value)}</dd>
                  </div>
                ))}
            </dl>
          )}
        </div>
      )}
    </Panel>
  );
}

function prettyValue(key: string, v: string): string {
  if (/^(Download|Installed) Size$/i.test(key)) return v;
  if (/^(size)$/i.test(key) && /^\d+$/.test(v)) return formatBytes(Number(v));
  return v;
}
