/** Line diff for the Diff tab and the changed-line markers. */

export interface DiffRow {
  type: 'same' | 'add' | 'del';
  text: string;
  /** 1-based line number in the old text (not for added lines). */
  oldNo?: number;
  /** 1-based line number in the new text (not for removed lines). */
  newNo?: number;
}

const lines = (s: string): string[] => {
  const a = s.replace(/\r\n/g, '\n').split('\n');
  if (a.length && a[a.length - 1] === '') a.pop();
  return a;
};

export function diffLines(oldText: string, newText: string): DiffRow[] {
  const a = lines(oldText);
  const b = lines(newText);
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let ea = a.length;
  let eb = b.length;
  while (ea > start && eb > start && a[ea - 1] === b[eb - 1]) {
    ea--;
    eb--;
  }
  const rows: DiffRow[] = [];
  for (let i = 0; i < start; i++) rows.push({ type: 'same', text: a[i], oldNo: i + 1, newNo: i + 1 });
  const ma = a.slice(start, ea);
  const mb = b.slice(start, eb);
  const n = ma.length;
  const m = mb.length;
  if (n * m > 4_000_000) {
    // Too large for the table: show it as one replaced block.
    ma.forEach((t, i) => rows.push({ type: 'del', text: t, oldNo: start + i + 1 }));
    mb.forEach((t, i) => rows.push({ type: 'add', text: t, newNo: start + i + 1 }));
  } else {
    const w = m + 1;
    const lcs = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) lcs[i * w + j] = ma[i] === mb[j] ? lcs[(i + 1) * w + j + 1] + 1 : Math.max(lcs[(i + 1) * w + j], lcs[i * w + j + 1]);
    let i = 0;
    let j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && ma[i] === mb[j]) {
        rows.push({ type: 'same', text: ma[i], oldNo: start + i + 1, newNo: start + j + 1 });
        i++;
        j++;
      } else if (j < m && (i === n || lcs[i * w + j + 1] >= lcs[(i + 1) * w + j])) {
        rows.push({ type: 'add', text: mb[j], newNo: start + j + 1 });
        j++;
      } else {
        rows.push({ type: 'del', text: ma[i], oldNo: start + i + 1 });
        i++;
      }
    }
  }
  for (let k = 0; ea + k < a.length; k++) rows.push({ type: 'same', text: a[ea + k], oldNo: ea + k + 1, newNo: eb + k + 1 });
  return rows;
}

export interface DiffSummary {
  added: number;
  removed: number;
  /** 1-based line numbers in the new text that were added or changed. */
  changedLines: Set<number>;
}

export function summarize(rows: DiffRow[]): DiffSummary {
  const changedLines = new Set<number>();
  let added = 0;
  let removed = 0;
  for (const r of rows) {
    if (r.type === 'add') {
      added++;
      changedLines.add(r.newNo!);
    } else if (r.type === 'del') removed++;
  }
  return { added, removed, changedLines };
}
