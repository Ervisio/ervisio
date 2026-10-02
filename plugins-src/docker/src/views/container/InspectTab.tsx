import { useMemo, useState, type ReactNode } from 'react';
import { docker, errorText } from '../../api/engine';
import { useAsync } from '../../api/hooks';
import type { ContainerInspect } from '../../api/types';
import { t } from '../../i18n';
import { Button, Icon, Input, Segmented, Skeleton, toast } from '../../kit';
import { copyText } from './util';

/* ---------- JSON tree ---------- */

type Json = string | number | boolean | null | Json[] | { [k: string]: Json };
const isObj = (v: Json): v is { [k: string]: Json } | Json[] => v !== null && typeof v === 'object';
const entriesOf = (v: Json): [string, Json][] => (Array.isArray(v) ? v.map((x, i) => [String(i), x] as [string, Json]) : Object.entries(v as object) as [string, Json][]);

/** True when the key or any value below `v` contains the needle. */
function contains(key: string, v: Json, needle: string): boolean {
  if (key.toLowerCase().includes(needle)) return true;
  if (!isObj(v)) return String(v).toLowerCase().includes(needle);
  return entriesOf(v).some(([k, x]) => contains(k, x, needle));
}

function mark(text: string, needle: string): ReactNode {
  if (!needle) return text;
  const i = text.toLowerCase().indexOf(needle);
  if (i < 0) return text;
  return <>{text.slice(0, i)}<mark>{text.slice(i, i + needle.length)}</mark>{text.slice(i + needle.length)}</>;
}

function Node({ k, v, depth, needle, root }: { k: string; v: Json; depth: number; needle: string; root?: boolean }) {
  const [open, setOpen] = useState(depth < 1);
  const key = root ? null : <span className="dk-c-j-k">{mark(JSON.stringify(k), needle)}</span>;
  if (!isObj(v)) {
    const cls = typeof v === 'string' ? 'dk-c-j-s' : v === null ? 'dk-c-j-n' : 'dk-c-j-v';
    return (
      <div className="dk-c-j-line">
        {key}{key && ': '}<span className={cls}>{mark(typeof v === 'string' ? JSON.stringify(v) : String(v), needle)}</span>
      </div>
    );
  }
  const list = entriesOf(v);
  const arr = Array.isArray(v);
  const forced = !!needle && contains(k, v, needle);
  const expanded = needle ? forced : open;
  const kids = needle ? list.filter(([ck, x]) => contains(ck, x, needle)) : list;
  return (
    <div className="dk-c-j-node">
      <button type="button" className="dk-c-j-line dk-c-j-tog" aria-expanded={expanded} onClick={() => setOpen(!open)} disabled={!!needle}>
        <Icon name="chevron" className={`dk-c-j-ch${expanded ? ' dk-c-j-ch--open' : ''}`} />
        {key}{key && ': '}<span className="dk-c-j-p">{arr ? '[' : '{'}</span>
        {!expanded && <span className="dk-muted"> {list.length} {arr ? t('container.inspect.items') : t('container.inspect.keys')} </span>}
        {!expanded && <span className="dk-c-j-p">{arr ? ']' : '}'}</span>}
      </button>
      {expanded && (
        <>
          <div className="dk-c-j-kids">{kids.map(([ck, x]) => <Node key={ck} k={ck} v={x} depth={depth + 1} needle={needle} />)}</div>
          <div className="dk-c-j-line dk-c-j-p">{arr ? ']' : '}'}</div>
        </>
      )}
    </div>
  );
}

interface Change {
  Path: string;
  Kind: 0 | 1 | 2;
}
const KIND: Record<number, ['C' | 'A' | 'D', string]> = { 0: ['C', 'changed'], 1: ['A', 'added'], 2: ['D', 'deleted'] };

export function InspectTab({ id, inspect }: { id: string; inspect: ContainerInspect }) {
  const [view, setView] = useState<'json' | 'changes'>('json');
  return (
    <div className="dk-card">
      <div className="dk-c-lgbar">
        <Segmented
          aria-label={t('container.tab.inspect')}
          options={[{ value: 'json', label: t('container.inspect.json') }, { value: 'changes', label: t('container.inspect.changes') }]}
          value={view}
          onChange={(v) => setView(v as 'json' | 'changes')}
        />
      </div>
      {view === 'json' ? <JsonView inspect={inspect} /> : <Changes id={id} />}
    </div>
  );
}

function JsonView({ inspect }: { inspect: ContainerInspect }) {
  const [q, setQ] = useState('');
  const needle = q.trim().toLowerCase();
  const root = inspect as unknown as Json;
  const hits = useMemo(() => (needle ? contains('', root, needle) : true), [needle, root]);
  const copy = async () => {
    if (await copyText(JSON.stringify(inspect, null, 2))) toast.ok(t('container.inspect.copied'));
    else toast.err(t('container.copyFail'));
  };
  return (
    <>
      <div className="dk-c-lgbar">
        <div className="dk-c-lg-search"><Input compact icon="search" placeholder={t('container.inspect.search')} aria-label={t('container.inspect.search')} value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <Button size="sm" icon="copy" onClick={copy}>{t('container.inspect.copy')}</Button>
      </div>
      <div className="dk-c-json" role="tree">
        {hits ? <Node k="" v={root} depth={0} needle={needle} root /> : <p className="dk-muted">{t('container.inspect.noMatch')}</p>}
      </div>
    </>
  );
}

function Changes({ id }: { id: string }) {
  const { data, error, loading, reload } = useAsync(async () => (await docker.get<Change[] | null>(`/containers/${encodeURIComponent(id)}/changes`)) ?? [], [id]);
  const [q, setQ] = useState('');
  const needle = q.trim().toLowerCase();
  const list = (data ?? []).filter((c) => !needle || c.Path.toLowerCase().includes(needle));
  return (
    <>
      <div className="dk-c-lgbar">
        <div className="dk-c-lg-search"><Input compact icon="search" placeholder={t('container.inspect.searchPaths')} aria-label={t('container.inspect.searchPaths')} value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <Button size="sm" icon="refresh" onClick={reload}>{t('common.refresh')}</Button>
      </div>
      <p className="dk-muted">{t('container.inspect.changesNote')}</p>
      {loading && !data ? <Skeleton lines={4} /> : error ? <p className="dk-c-err">{errorText(error)}</p> : list.length === 0 ? (
        <p className="dk-muted">{data?.length ? t('container.inspect.noMatch') : t('container.inspect.noChanges')}</p>
      ) : (
        <div className="dk-c-changes">
          {list.slice(0, 1000).map((c, i) => {
            const [letter, label] = KIND[c.Kind] ?? KIND[0];
            return (
              <div key={`${c.Path}-${i}`} className="dk-c-row">
                <span className={`dk-c-kind dk-c-kind--${letter}`} title={t(`container.inspect.kind.${label}`)}>{letter}</span>
                <span className="dk-mono dk-c-src">{c.Path}</span>
              </div>
            );
          })}
          {list.length > 1000 && <p className="dk-muted">{t('container.inspect.firstN', { n: 1000, total: list.length })}</p>}
        </div>
      )}
    </>
  );
}
