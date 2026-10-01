import { useMemo, useState } from 'react';
import { useI18n, useT } from '../../i18n';
import { Button, Chip, EmptyState, Input, Skeleton, Table, type Column } from '../../ui';
import { formatBytes } from '../../lib/format';
import { SourceBadge, displayName } from './helpers';
import { useSoftware } from './store';
import type { Pkg } from './types';

type Filter = 'all' | 'explicit' | 'deps' | 'orphans' | 'aur' | 'flatpak';
const PAGE = 300;

const key = (p: Pkg) => `${p.kind}:${p.scope ?? ''}:${p.name}`;

export function InstalledTab({ activeKey, onOpen, onRemove, busy }: { activeKey?: string; onOpen(p: Pkg): void; onRemove(list: Pkg[]): void; busy: boolean }) {
  const t = useT('software');
  const { lang } = useI18n();
  const { installed, error } = useSoftware();
  const [filter, setFilter] = useState<Filter>('all');
  const [q, setQ] = useState('');
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [limit, setLimit] = useState(PAGE);

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: 0, explicit: 0, deps: 0, orphans: 0, aur: 0, flatpak: 0 };
    for (const p of installed ?? []) {
      c.all++;
      if (p.reason === 'explicit' && p.kind !== 'flatpak') c.explicit++;
      if (p.reason === 'dependency') c.deps++;
      if (p.orphan) c.orphans++;
      if (p.kind === 'aur') c.aur++;
      if (p.kind === 'flatpak') c.flatpak++;
    }
    return c;
  }, [installed]);

  const rows = useMemo(() => {
    const low = q.trim().toLowerCase();
    return (installed ?? []).filter((p) => {
      switch (filter) {
        case 'explicit': if (!(p.reason === 'explicit' && p.kind !== 'flatpak')) return false; break;
        case 'deps': if (p.reason !== 'dependency') return false; break;
        case 'orphans': if (!p.orphan) return false; break;
        case 'aur': if (p.kind !== 'aur') return false; break;
        case 'flatpak': if (p.kind !== 'flatpak') return false; break;
      }
      return !low || p.name.toLowerCase().includes(low) || (p.title ?? '').toLowerCase().includes(low) || p.description.toLowerCase().includes(low);
    });
  }, [installed, filter, q]);

  const date = useMemo(() => new Intl.DateTimeFormat(lang, { day: 'numeric', month: 'short', year: 'numeric' }), [lang]);
  const visible = rows.slice(0, limit);

  const columns: Column<Pkg>[] = [
    {
      key: 'name', header: t('col.package'), sortable: true, value: (p) => displayName(p).toLowerCase(),
      render: (p) => (
        <span className="sw-nm"><b>{displayName(p)}</b>{p.title && <span className="sw-muted sw-mono"> {p.name}</span>}</span>
      ),
    },
    { key: 'version', header: t('col.version'), render: (p) => <span className="sw-ver">{p.version}</span> },
    { key: 'source', header: t('col.source'), sortable: true, value: (p) => p.source, render: (p) => <SourceBadge kind={p.kind} source={p.source} /> },
    { key: 'size', header: t('col.size'), sortable: true, align: 'right', value: (p) => p.size, render: (p) => <span className="sw-muted">{p.size ? formatBytes(p.size) : '-'}</span> },
    {
      key: 'reason', header: t('col.reason'), sortable: true, value: (p) => p.reason,
      render: (p) => (p.reason === 'explicit' ? t('reason.explicit') : <span className="sw-muted">{p.orphan ? t('reason.orphan') : t('reason.dependency')}</span>),
    },
    { key: 'date', header: t('col.installed'), sortable: true, value: (p) => p.installDate, render: (p) => <span className="sw-muted">{p.installDate ? date.format(new Date(p.installDate * 1000)) : '-'}</span> },
  ];

  const chips: { id: Filter; label: string }[] = [
    { id: 'all', label: t('filter.all') },
    { id: 'explicit', label: t('filter.explicit') },
    { id: 'deps', label: t('filter.deps') },
    { id: 'orphans', label: t('filter.orphans') },
    { id: 'aur', label: t('filter.aur') },
    { id: 'flatpak', label: t('filter.flatpak') },
  ];

  const chosen = (installed ?? []).filter((p) => sel.has(key(p)));

  return (
    <div className="sw-body sw-body--table">
      <div className="sw-tbar">
        <div className="sw-chips">
          {chips
            .filter((c) => c.id === 'all' || counts[c.id] > 0)
            .map((c) => (
              <Chip key={c.id} pressed={filter === c.id} count={counts[c.id]} onClick={() => { setFilter(c.id); setLimit(PAGE); setSel(new Set()); }}>
                {c.label}
              </Chip>
            ))}
        </div>
        <span className="sw-sp" />
        {chosen.length > 0 && (
          <Button variant="danger" icon="trash" disabled={busy} onClick={() => onRemove(chosen)}>{t('removeSelected', { count: chosen.length })}</Button>
        )}
        <Input compact icon="search" placeholder={t('filterInstalled')} aria-label={t('filterInstalled')} value={q} onChange={(e) => { setQ(e.target.value); setLimit(PAGE); }} fieldClassName="sw-filter" />
      </div>
      {!installed ? (
        error ? <EmptyState icon="alert" title={t('error.load')} text={error.message} /> : <Skeleton lines={8} height={40} />
      ) : (
        <>
          <Table
            columns={columns}
            rows={visible}
            rowKey={key}
            selectable
            selected={sel}
            onSelectedChange={setSel}
            onRowClick={onOpen}
            activeKey={activeKey}
            caption={t('tab.installed')}
            empty={<EmptyState icon="search" hue="sw" title={t('installed.none')} text={t('installed.noneText')} />}
          />
          {rows.length > limit && (
            <button type="button" className="sw-more" onClick={() => setLimit((l) => l + PAGE)}>{t('installed.more', { count: rows.length - limit })}</button>
          )}
        </>
      )}
    </div>
  );
}
