import { useI18n, useT } from '../../i18n';
import { useNavigate } from 'react-router-dom';
import {
  Badge,
  DropdownMenu,
  Icon,
  IconButton,
  Switch,
  type Column,
  type MenuItem,
} from '../../ui';
import { bootMode, short, stateKey, stateTone } from './helpers';
import type { Action, PanelTab, Unit, UnitKind } from './types';
import { formatBytes, relativeTime } from '../../lib/format';

type T = ReturnType<typeof useT>;

export const isRunning = (u: Unit) => u.state === 'running' || u.state === 'finished';
const SOCKET_TYPE_SUFFIX = / \((Stream|Datagram|SequentialPacket|Netlink|Special|FIFO)\)$/;

type MenuDeps = {
  win: boolean;
  t: T;
  run: (name: string, a: Action) => void;
  openUnit: (name: string, tab: PanelTab) => void;
  openLogs: (name: string) => void;
};

function unitMenu(u: Unit, { win, t, run, openUnit, openLogs }: MenuDeps): MenuItem[] {
  const running = isRunning(u);
  const isMasked = win ? u.enabled === 'masked' : u.load === 'masked';
  return [
    running
      ? { id: 'stop', label: t('actions.stop'), icon: 'power', onSelect: () => run(u.name, 'stop') }
      : {
          id: 'start',
          label: t('actions.start'),
          icon: 'play',
          onSelect: () => run(u.name, 'start'),
        },
    {
      id: 'restart',
      label: t('actions.restart'),
      icon: 'refresh',
      onSelect: () => run(u.name, 'restart'),
    },
    ...(win
      ? [
          u.sub === 'paused'
            ? {
                id: 'continue',
                label: t('actions.continue'),
                icon: 'play' as const,
                onSelect: () => run(u.name, 'continue'),
              }
            : {
                id: 'pause',
                label: t('actions.pause'),
                icon: 'power' as const,
                disabled: !running,
                onSelect: () => run(u.name, 'pause'),
              },
        ]
      : [
          {
            id: 'reload',
            label: t('actions.reload'),
            icon: 'refresh' as const,
            disabled: !running,
            onSelect: () => run(u.name, 'reload'),
          },
          { type: 'separator' as const },
          {
            id: 'logs',
            label: t('rowMenu.logs'),
            icon: 'logs' as const,
            onSelect: () => openUnit(u.name, 'logs'),
          },
          {
            id: 'file',
            label: t('rowMenu.unitFile'),
            icon: 'file' as const,
            onSelect: () => openUnit(u.name, 'unit'),
          },
          {
            id: 'inlogs',
            label: t('openInLogs'),
            icon: 'externallink' as const,
            onSelect: () => openLogs(u.name),
          },
        ]),
    { type: 'separator' },
    isMasked
      ? {
          id: 'unmask',
          label: win ? t('win.unmask') : t('actions.unmask'),
          icon: 'unlock',
          onSelect: () => run(u.name, 'unmask'),
        }
      : {
          id: 'mask',
          label: win ? t('win.mask') : t('actions.mask'),
          icon: 'lock',
          danger: true,
          onSelect: () => run(u.name, 'mask'),
        },
  ];
}

const serviceColumns = (
  cpu: Record<string, number>,
  t: T,
  dash: React.ReactNode,
): Column<Unit>[] => [
  {
    key: 'mem',
    header: t('col.memory'),
    sortable: true,
    mono: true,
    value: (u) => u.memory ?? -1,
    render: (u) => (u.memory != null ? formatBytes(u.memory) : dash),
  },
  {
    key: 'cpu',
    header: t('col.cpu'),
    sortable: true,
    mono: true,
    value: (u) => cpu[u.name] ?? -1,
    render: (u) =>
      cpu[u.name] != null && u.state === 'running' ? `${cpu[u.name].toFixed(1)}%` : dash,
  },
];

function timerColumns(compact: boolean, lang: string, t: T, dash: React.ReactNode): Column<Unit>[] {
  const next: Column<Unit> = {
    key: 'next',
    header: t('col.next'),
    sortable: true,
    value: (u) => u.next ?? 0,
    render: (u) => (u.next ? relativeTime(u.next * 1000, lang) : dash),
  };
  if (compact) return [next];
  return [
    next,
    {
      key: 'last',
      header: t('col.last'),
      sortable: true,
      value: (u) => u.last ?? 0,
      render: (u) => (u.last ? relativeTime(u.last * 1000, lang) : dash),
    },
  ];
}

const socketColumns = (t: T, dash: React.ReactNode): Column<Unit>[] => [
  {
    key: 'listen',
    header: t('col.listen'),
    mono: true,
    render: (u) =>
      u.listen?.length ? u.listen.map((l) => l.replace(SOCKET_TYPE_SUFFIX, '')).join(', ') : dash,
  },
];

/** Columns between "boot" and the row actions; they depend on the unit kind and the room left. */
function kindColumns(
  kind: UnitKind,
  compact: boolean,
  cpu: Record<string, number>,
  lang: string,
  t: T,
  dash: React.ReactNode,
): Column<Unit>[] {
  if (kind === 'timer') return timerColumns(compact, lang, t, dash);
  if (compact) return [];
  return kind === 'service' ? serviceColumns(cpu, t, dash) : socketColumns(t, dash);
}

function UnitCard({
  u,
  bad,
  busy,
  active,
  mem,
  cpu,
  t,
  boot,
  menu,
  onOpen,
  onAct,
  onLogs,
}: {
  u: Unit;
  bad: boolean;
  busy: boolean;
  active: boolean;
  mem: number | null;
  cpu?: number;
  t: ReturnType<typeof useT>;
  boot: React.ReactNode;
  menu: MenuItem[];
  onOpen(): void;
  onAct(a: Action): void;
  onLogs?: () => void;
}) {
  const running = u.state === 'running' || u.state === 'finished';
  return (
    <div className={`svc-card${bad ? ' svc-card--bad' : ''}${active ? ' svc-card--on' : ''}`}>
      <button className="svc-card-hd" type="button" onClick={onOpen}>
        <span className={`svc-tile svc-tile--${bad ? 'bad' : running ? 'ok' : 'off'}`}>
          <Icon name="services" />
        </span>
        <span className="svc-card-tx">
          <b>{u.name}</b>
          <small>{u.description}</small>
        </span>
      </button>
      <div className="svc-card-m">
        <Badge tone={stateTone(u)}>{t(`state.${stateKey(u)}`)}</Badge>
        <span className="svc-muted svc-mono">
          {mem != null ? formatBytes(mem) : ''}
          {mem != null && cpu != null && u.state === 'running' ? ` · ${cpu.toFixed(1)}%` : ''}
        </span>
      </div>
      <div className="svc-card-f">
        <label className="svc-boot">
          {boot}
          <span>{t('boot.short')}</span>
        </label>
        <div className="svc-q">
          <IconButton
            icon="refresh"
            label={t('actions.restart')}
            disabled={busy}
            onClick={() => onAct('restart')}
          />
          <IconButton
            icon={running ? 'power' : 'play'}
            label={running ? t('actions.stop') : t('actions.start')}
            disabled={busy}
            onClick={() => onAct(running ? 'stop' : 'start')}
          />
          {onLogs && <IconButton icon="logs" label={t('rowMenu.logs')} onClick={onLogs} />}
          <DropdownMenu
            items={menu}
            trigger={(p) => <IconButton icon="more" label={t('more')} {...p} />}
          />
        </div>
      </div>
    </div>
  );
}

type CellDeps = {
  kind: UnitKind;
  win: boolean;
  t: T;
  run: (name: string, a: Action) => void;
  busy: ReadonlySet<string>;
};

function bootCell(u: Unit, { win, t, run, busy }: CellDeps) {
  const bootLabel = (v: string) =>
    win ? t(`win.boot.${v}`) || t(`boot.${v}`) : v ? t(`boot.${v}`) : '';
  const m = bootMode(u.enabled);
  if (m === 'fixed')
    return <span className="svc-fixed">{u.enabled ? bootLabel(u.enabled) : '—'}</span>;
  return (
    <Switch
      checked={m === 'on'}
      disabled={busy.has(u.name)}
      onChange={() => run(u.name, m === 'on' ? 'disable' : 'enable')}
      aria-label={t('boot.label', { name: short(u.name) })}
    />
  );
}

function rowActions(u: Unit, menu: MenuItem[], { kind, t, run, busy }: CellDeps) {
  return (
    <span className="svc-rowact" onClick={(e) => e.stopPropagation()}>
      {kind === 'service' && (
        <IconButton
          icon="refresh"
          label={t('actions.restart')}
          disabled={busy.has(u.name)}
          onClick={() => run(u.name, 'restart')}
        />
      )}
      <DropdownMenu
        items={menu}
        aria-label={t('rowMenu.label', { name: short(u.name) })}
        trigger={(p) => <IconButton icon="more" label={t('more')} {...p} />}
      />
    </span>
  );
}

const nameCell = (u: Unit) => (
  <div className="svc-nm">
    <b>{u.name}</b>
    <small>{u.description}</small>
  </div>
);

type ServiceCellsArgs = CellDeps & {
  compact: boolean;
  selected: string | null;
  cpu: Record<string, number>;
  openUnit: (name: string, tab?: PanelTab) => void;
};

/** Table columns and card renderer of the unit list; both share the boot switch and the row menu. */
export function useServiceCells({ compact, selected, cpu, openUnit, ...deps }: ServiceCellsArgs) {
  const { kind, win, t, run, busy } = deps;
  const { lang } = useI18n();
  const nav = useNavigate();

  const menuFor = (u: Unit) =>
    unitMenu(u, {
      win,
      t,
      run,
      openUnit,
      openLogs: (n) => nav(`/logs?unit=${encodeURIComponent(n)}`),
    });
  const stateCell = (u: Unit) => <Badge tone={stateTone(u)}>{t(`state.${stateKey(u)}`)}</Badge>;
  const dash = <span className="svc-muted">—</span>;

  const columns: Column<Unit>[] = [
    { key: 'name', header: t('col.name'), sortable: true, value: (u) => u.name, render: nameCell },
    {
      key: 'state',
      header: t('col.state'),
      sortable: true,
      value: (u) => u.state,
      render: stateCell,
    },
    { key: 'boot', header: t('col.boot'), render: (u) => bootCell(u, deps) },
    ...kindColumns(kind, compact, cpu, lang, t, dash),
    { key: 'act', header: '', align: 'right', render: (u) => rowActions(u, menuFor(u), deps) },
  ];

  const renderCard = (u: Unit) => (
    <UnitCard
      key={u.name}
      u={u}
      bad={u.state === 'failed'}
      busy={busy.has(u.name)}
      active={selected === u.name}
      mem={u.memory}
      cpu={cpu[u.name]}
      t={t}
      boot={bootCell(u, deps)}
      menu={menuFor(u)}
      onOpen={() => openUnit(u.name)}
      onAct={(a) => run(u.name, a)}
      onLogs={win ? undefined : () => openUnit(u.name, 'logs')}
    />
  );

  return { columns, renderCard };
}
