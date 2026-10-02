import { useMemo } from 'react';
import { t } from '../../i18n';
import { diffLines } from './diff';
import { highlightLine } from './highlight';

const CONTEXT = 3;

/** Unified diff of the compose file against the last deployed copy, with unchanged runs folded. */
export function DiffView({ oldText, newText }: { oldText: string; newText: string }) {
  const rows = useMemo(() => diffLines(oldText, newText), [oldText, newText]);
  const shown = useMemo(() => {
    const keep = new Array<boolean>(rows.length).fill(false);
    rows.forEach((r, i) => {
      if (r.type !== 'same') for (let k = Math.max(0, i - CONTEXT); k <= Math.min(rows.length - 1, i + CONTEXT); k++) keep[k] = true;
    });
    const out: ({ row: (typeof rows)[number] } | { fold: number })[] = [];
    let skipped = 0;
    rows.forEach((r, i) => {
      if (keep[i]) {
        if (skipped) out.push({ fold: skipped });
        skipped = 0;
        out.push({ row: r });
      } else skipped++;
    });
    if (skipped) out.push({ fold: skipped });
    return out;
  }, [rows]);
  if (!rows.some((r) => r.type !== 'same')) return <p className="dk-muted dk-sk-nodiff">{t('stacks.diff.none')}</p>;
  return (
    <div className="dk-ed">
      <div className="dk-ed-scroll dk-diff">
        {shown.map((x, i) =>
          'fold' in x ? (
            <div key={i} className="dk-diff-fold">{t('stacks.diff.fold', { n: x.fold })}</div>
          ) : (
            <div key={i} className={`dk-diff-r dk-diff-r--${x.row.type}`}>
              <span className="dk-diff-n">{x.row.oldNo ?? ''}</span>
              <span className="dk-diff-n">{x.row.newNo ?? ''}</span>
              <span className="dk-diff-m">{x.row.type === 'add' ? '+' : x.row.type === 'del' ? '-' : ''}</span>
              <span className="dk-diff-t" dangerouslySetInnerHTML={{ __html: highlightLine(x.row.text, 'yaml') || '​' }} />
            </div>
          ),
        )}
      </div>
    </div>
  );
}
