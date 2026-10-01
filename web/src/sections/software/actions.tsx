import { useCallback, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT } from '../../i18n';
import { Badge, Button, ConfirmDialog, Dialog } from '../../ui';
import { formatBytes } from '../../lib/format';
import { useSoftware } from './store';
import { startJob, useTx } from './tx';
import { SourceBadge, displayName, updateKey } from './helpers';
import type { Pkg, SearchResult, Update } from './types';
import type { TxParams } from './types';

/** Everything that changes the system goes through here, with its confirmation dialogs. */
export function useActions() {
  const t = useT('software');
  const nav = useNavigate();
  const { summary, updates } = useSoftware();
  const tx = useTx();
  const [install, setInstall] = useState<SearchResult | null>(null);
  const [remove, setRemove] = useState<Pkg[] | null>(null);
  const [partial, setPartial] = useState<{ names: string[] } | null>(null);
  const manager = summary?.manager ?? '';

  /** Open the Terminal; with cmd, a new session gets it typed at the prompt (not run). */
  const openTerminal = useCallback((cmd?: string) => nav(cmd ? `/terminal?cmd=${encodeURIComponent(cmd)}` : '/terminal'), [nav]);
  const aurHelper = summary?.aurHelper || 'yay';

  /** Upgrade everything (system packages + system Flatpaks, and user Flatpaks). */
  const updateAll = useCallback(() => {
    const steps: TxParams[] = [{ op: 'upgrade', packages: [], source: 'all' }];
    if ((updates ?? []).some((u) => u.kind === 'flatpak' && u.scope === 'user')) steps.push({ op: 'upgrade', packages: [], source: 'flatpak', scope: 'user' });
    startJob({ title: { key: 'updateAll' }, steps });
  }, [updates]);

  const runUpdates = useCallback((list: Update[]) => {
    const steps: TxParams[] = [];
    const repo = list.filter((u) => u.kind === 'repo').map((u) => u.name);
    if (repo.length) steps.push({ op: 'upgrade', packages: repo, source: 'repo' });
    for (const scope of ['system', 'user'] as const) {
      const fl = list.filter((u) => u.kind === 'flatpak' && (u.scope ?? 'system') === scope).map((u) => u.name);
      if (fl.length) steps.push({ op: 'upgrade', packages: fl, source: 'flatpak', scope });
    }
    if (!steps.length) return;
    startJob({ title: list.length === 1 ? { key: 'update', vars: { name: displayName(list[0]) } } : { key: 'updateSelected', vars: { count: list.length } }, steps });
  }, []);

  /** Update some packages. On Arch a partial upgrade can break the system, so ask first. */
  const updateSome = useCallback(
    (list: Update[]) => {
      const repo = list.filter((u) => u.kind === 'repo');
      const allRepo = (updates ?? []).filter((u) => u.kind === 'repo');
      if (manager === 'pacman' && repo.length > 0 && repo.length < allRepo.length) {
        setPartial({ names: repo.map((u) => u.name) });
        return;
      }
      runUpdates(list);
    },
    [manager, updates, runUpdates],
  );

  const confirmInstall = useCallback(() => {
    const r = install;
    setInstall(null);
    if (!r) return;
    const ref = r.kind === 'flatpak' && r.remote ? `${r.remote}/${r.name}` : r.name;
    startJob({
      title: { key: 'install', vars: { name: displayName(r) } },
      steps: [{ op: 'install', packages: [ref], source: r.kind === 'flatpak' ? 'flatpak' : 'repo', scope: r.kind === 'flatpak' ? 'system' : undefined }],
    });
  }, [install]);

  const confirmRemove = useCallback(() => {
    const list = remove;
    setRemove(null);
    if (!list?.length) return;
    const steps: TxParams[] = [];
    const sys = list.filter((p) => p.kind !== 'flatpak').map((p) => p.name);
    if (sys.length) steps.push({ op: 'remove', packages: sys, source: 'repo' });
    for (const scope of ['system', 'user'] as const) {
      const fl = list.filter((p) => p.kind === 'flatpak' && (p.scope ?? 'system') === scope).map((p) => p.name);
      if (fl.length) steps.push({ op: 'remove', packages: fl, source: 'flatpak', scope });
    }
    startJob({ title: list.length === 1 ? { key: 'remove', vars: { name: displayName(list[0]) } } : { key: 'removeMany', vars: { count: list.length } }, steps });
  }, [remove]);

  const busy = tx.phase === 'running';

  const ui: ReactNode = (
    <>
      <Dialog
        open={!!install}
        onClose={() => setInstall(null)}
        title={t('install.title', { name: install ? displayName(install) : '' })}
        description={t('install.text')}
        icon="download"
        footer={
          <>
            <Button onClick={() => setInstall(null)}>{t('cancel')}</Button>
            <Button variant="primary" icon="download" onClick={confirmInstall}>{t('install.confirm')}</Button>
          </>
        }
      >
        {install && (
          <dl className="sw-kv">
            <dt>{t('install.source')}</dt>
            <dd><SourceBadge kind={install.kind} source={install.source} />{install.kind === 'flatpak' && install.remote ? <span className="sw-muted"> {install.remote}</span> : null}</dd>
            <dt>{t('install.version')}</dt>
            <dd className="sw-mono">{install.version || '-'}</dd>
            <dt>{t('install.package')}</dt>
            <dd className="sw-mono">{install.name}</dd>
            <dt>{t('install.runsAs')}</dt>
            <dd>{t('install.admin')}</dd>
          </dl>
        )}
        {install?.kind === 'repo' && <p className="sw-hint">{t('install.deps')}</p>}
      </Dialog>

      <ConfirmDialog
        open={!!remove}
        onClose={() => setRemove(null)}
        onConfirm={confirmRemove}
        title={remove && remove.length === 1 ? t('remove.title', { name: displayName(remove[0]) }) : t('remove.titleMany', { count: remove?.length ?? 0 })}
        description={t('remove.text')}
        confirmLabel={t('remove.confirm')}
        confirmText={remove && remove.length === 1 ? remove[0].name : t('remove.word')}
        icon="trash"
      >
        {remove && remove.length > 1 && (
          <div className="sw-chiplist">
            {remove.slice(0, 12).map((p) => (
              <Badge key={p.name} tone="neutral" dot={false}>{displayName(p)}</Badge>
            ))}
            {remove.length > 12 && <Badge tone="neutral" dot={false}>+{remove.length - 12}</Badge>}
          </div>
        )}
      </ConfirmDialog>

      <Dialog
        open={!!partial}
        onClose={() => setPartial(null)}
        title={t('partial.title')}
        description={t('partial.text')}
        icon="alert"
        tone="warn"
        footer={
          <>
            <Button onClick={() => setPartial(null)}>{t('cancel')}</Button>
            <Button
              variant="primary"
              icon="download"
              onClick={() => {
                setPartial(null);
                updateAll();
              }}
            >
              {t('partial.all', { count: (updates ?? []).filter((u) => u.kind === 'repo').length })}
            </Button>
          </>
        }
      >
        <p className="sw-hint">
          {t('partial.size', { size: formatBytes((updates ?? []).filter((u) => u.kind === 'repo').reduce((a, u) => a + u.size, 0)) })}
        </p>
      </Dialog>
    </>
  );

  return { ui, busy, openTerminal, aurHelper, updateAll, updateSome, runUpdates, askInstall: setInstall, askRemove: setRemove, updateKey };
}
