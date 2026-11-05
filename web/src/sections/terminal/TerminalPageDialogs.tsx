import { useT } from '../../i18n';
import { ConfirmDialog } from '../../ui';
import { HostDialog, RenameDialog, SnippetDialog } from './Dialogs';
import type {
  useTerminalHosts,
  useTerminalSessionDialogs,
  useTerminalSnippets,
} from './useTerminalActions';

interface Props {
  sessionDialogs: ReturnType<typeof useTerminalSessionDialogs>;
  hostsCtl: ReturnType<typeof useTerminalHosts>;
  snippetsCtl: ReturnType<typeof useTerminalSnippets>;
}

export function TerminalPageDialogs({ sessionDialogs, hostsCtl, snippetsCtl }: Props) {
  const t = useT('terminal');
  const { renaming, setRenaming, killing, setKilling, doRename, doKill } = sessionDialogs;
  const { hostDlg, setHostDlg, submitHost } = hostsCtl;
  const { snipDlg, setSnipDlg, saveSnippet } = snippetsCtl;
  return (
    <>
      <RenameDialog
        open={!!renaming}
        name={renaming?.name ?? ''}
        onClose={() => setRenaming(null)}
        onSave={doRename}
      />
      <ConfirmDialog
        open={!!killing}
        onClose={() => setKilling(null)}
        onConfirm={doKill}
        title={t('killTitle', { name: killing?.name ?? '' })}
        description={t('killText')}
        confirmLabel={t('kill')}
      />
      <HostDialog
        open={!!hostDlg}
        mode={hostDlg?.mode ?? 'connect'}
        initial={hostDlg?.host}
        onClose={() => setHostDlg(null)}
        onSubmit={submitHost}
      />
      <SnippetDialog
        open={!!snipDlg}
        initial={snipDlg?.snippet ?? (snipDlg?.command ? { command: snipDlg.command } : undefined)}
        onClose={() => setSnipDlg(null)}
        onSave={saveSnippet}
      />
    </>
  );
}
