import { useRef, useState } from 'react';
import { ApiError, call } from '../../api';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { errMsg } from './terminalErrors';
import {
  newId,
  type HistoryItem,
  type Host,
  type SessionInfo,
  type Snippet,
  type TermHandle,
} from './types';

const HISTORY_LIMIT = 50;

type Save = (key: string, value: unknown) => Promise<unknown>;

// Rename and kill confirmations for a session.
export function useTerminalSessionDialogs(t: TFn, refresh: () => Promise<void>) {
  const [renaming, setRenaming] = useState<SessionInfo | null>(null);
  const [killing, setKilling] = useState<SessionInfo | null>(null);

  const doRename = async (name: string) => {
    const s = renaming;
    setRenaming(null);
    if (!s) return;
    try {
      await call('terminal.rename', { id: s.id, name }, { admin: s.admin });
      toast.ok(t('renamed', { name }));
      void refresh();
    } catch (e) {
      toast.err(t('renameFailed', { error: errMsg(e) }));
    }
  };
  const doKill = async () => {
    const s = killing;
    setKilling(null);
    if (!s) return;
    try {
      await call('terminal.kill', { id: s.id }, { admin: s.admin });
      toast.ok(t('killed', { name: s.name }));
    } catch (e) {
      if (!(e instanceof ApiError && e.code === 'not_found'))
        toast.err(t('killFailed', { error: errMsg(e) }));
    }
    void refresh();
  };

  return { renaming, setRenaming, killing, setKilling, doRename, doKill };
}

export type HostDialogState = { mode: 'connect' | 'edit'; host: Host | null } | null;

interface HostsInput {
  t: TFn;
  hosts: Host[];
  save: Save;
  sessions: SessionInfo[];
  create(p: Record<string, unknown>): Promise<string | null>;
  showSession(id: string): void;
}

// Saved SSH hosts: connect, add/edit through the host dialog, delete with undo.
export function useTerminalHosts({ t, hosts, save, sessions, create, showSession }: HostsInput) {
  const [hostDlg, setHostDlg] = useState<HostDialogState>(null);

  const connectSsh = (h: Omit<Host, 'id'>) =>
    void create({ kind: 'ssh', host: h.host, user: h.user, port: h.port, name: h.name });
  const connectHost = (h: Host) => {
    const existing = sessions.find((s) => s.kind === 'ssh' && s.name === h.name);
    if (existing) return showSession(existing.id);
    connectSsh(h);
  };
  const submitHost = (h: Omit<Host, 'id'> & { id?: string }, saveIt: boolean) => {
    const mode = hostDlg?.mode;
    setHostDlg(null);
    if (saveIt || mode === 'edit') {
      const entry: Host = {
        id: h.id ?? newId(),
        name: h.name,
        host: h.host,
        user: h.user,
        port: h.port,
      };
      const next = hosts.some((x) => x.id === entry.id)
        ? hosts.map((x) => (x.id === entry.id ? entry : x))
        : [...hosts, entry];
      void save('terminal.hosts', next);
      if (mode === 'edit') return toast.ok(t('hostSaved', { name: entry.name }));
    }
    connectSsh(h);
  };
  const deleteHost = (h: Host) => {
    void save(
      'terminal.hosts',
      hosts.filter((x) => x.id !== h.id),
    );
    toast.undo(
      t('hostDeleted', { name: h.name }),
      t('undo'),
      () => void save('terminal.hosts', hosts),
    );
  };

  return { hostDlg, setHostDlg, connectHost, submitHost, deleteHost };
}

export type SnippetDialogState = { snippet?: Snippet; command?: string } | null;

interface SnippetsInput {
  t: TFn;
  snippets: Snippet[];
  opts: object;
  save: Save;
}

// Snippet list edits through the snippet dialog, plus the chips visibility toggle.
export function useTerminalSnippets({ t, snippets, opts, save }: SnippetsInput) {
  const [snipDlg, setSnipDlg] = useState<SnippetDialogState>(null);

  const saveSnippet = (title: string, command: string) => {
    const edit = snipDlg?.snippet;
    setSnipDlg(null);
    const next =
      edit && snippets.some((s) => s.id === edit.id)
        ? snippets.map((s) => (s.id === edit.id ? { ...s, title, command } : s))
        : [...snippets, { id: newId(), title, command }];
    void save('terminal.snippets', next);
    toast.ok(t('snippet.saved', { name: title }));
  };
  const deleteSnippet = (s: Snippet) => {
    void save(
      'terminal.snippets',
      snippets.filter((x) => x.id !== s.id),
    );
    toast.undo(
      t('snippet.deleted', { name: s.title }),
      t('undo'),
      () => void save('terminal.snippets', snippets),
    );
  };
  const setShowSnippets = (v: boolean) => {
    void save('terminal', { ...opts, snippets: v });
    void save('terminal.showSnippets', v);
  };

  return { snipDlg, setSnipDlg, saveSnippet, deleteSnippet, setShowSnippets };
}

// Sends commands to the active pane's terminal and records them in history.
export function useTerminalCommands(t: TFn, activePane: 0 | 1, history: HistoryItem[], save: Save) {
  const handles = useRef<[TermHandle | null, TermHandle | null]>([null, null]);

  const target = (): TermHandle | null => handles.current[activePane] ?? handles.current[0];
  const remember = (cmd: string) => {
    const c = cmd.trim();
    if (!c) return;
    void save(
      'terminal.history',
      [{ cmd: c, at: Date.now() }, ...history.filter((h) => h.cmd !== c)].slice(0, HISTORY_LIMIT),
    );
  };
  const runCmd = (cmd: string) => {
    const h = target();
    if (!h) return toast.info(t('noSession'));
    h.run(cmd);
    remember(cmd);
  };
  const pasteCmd = (cmd: string) => {
    const h = target();
    if (!h) return toast.info(t('noSession'));
    h.paste(cmd);
  };

  return { handles, target, runCmd, pasteCmd };
}
