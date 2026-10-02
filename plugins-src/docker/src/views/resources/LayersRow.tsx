import { useEffect, useState } from 'react';
import { docker } from '../../api/engine';
import { formatBytes, relativeTime } from '../../api/format';
import { t } from '../../i18n';
import { Skeleton } from '../../kit';
import { refPath } from './imageRef';

interface HistoryItem {
  Id: string;
  Created: number;
  CreatedBy: string;
  Size: number;
  Tags: string[] | null;
  Comment?: string;
}

/** "/bin/sh -c #(nop)  CMD [...]" -> "CMD [...]". */
const clean = (s: string): string => s.replace(/^\/bin\/sh -c (#\(nop\)\s*)?/, '').replace(/\s+/g, ' ').trim();

/** The contents of the expanded row of an image: its layers (history), newest first, with a size bar. */
export function LayersRow({ id }: { id: string }) {
  const [rows, setRows] = useState<HistoryItem[] | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => {
    let live = true;
    docker.get<HistoryItem[]>(`/images/${refPath(id)}/history`).then((r) => live && setRows(r ?? []), (e) => live && setErr((e as Error).message));
    return () => { live = false; };
  }, [id]);
  if (err) return <p className="dk-fail">{err}</p>;
  if (!rows) return <Skeleton lines={3} />;
  const max = Math.max(1, ...rows.map((r) => r.Size));
  return (
    <div className="dk-hist" role="list" aria-label={t('res.images.layers')}>
      {rows.map((r, i) => (
        <div key={i} className="dk-hist-r" role="listitem">
          <span className="dk-hist-sz">{r.Size > 0 ? formatBytes(r.Size) : '0 B'}</span>
          <span className="dk-hist-bar"><i style={{ width: `${Math.max(r.Size > 0 ? 2 : 0, (r.Size / max) * 100)}%` }} /></span>
          <code className="dk-hist-cmd" title={clean(r.CreatedBy)}>{clean(r.CreatedBy) || '–'}</code>
          <span className="dk-muted">{r.Created ? relativeTime(r.Created) : ''}</span>
        </div>
      ))}
    </div>
  );
}
