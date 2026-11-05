import type { TFn } from '../../i18n';
import { relativeTime } from '../../lib/format';
import { usePaletteActions } from '../../sections';
import type { IconName } from '../../ui';
import type { PaletteItem } from './Palette';
import { snippetLook } from './Snippets';
import type { HistoryItem, Kind, SessionInfo, Snippet } from './types';

function kindLook(kind: Kind): { icon: IconName; hue: string } {
  if (kind === 'root') return { icon: 'lock', hue: 'svc' };
  if (kind === 'ssh') return { icon: 'server', hue: 'ov' };
  return { icon: 'terminal', hue: 'term' };
}

interface TerminalPaletteActions {
  newLocal(): void;
  newRoot(): void;
  toggleFocus(): void;
}

// Entries the app-wide command palette shows for this section.
export function useTerminalPaletteActions(t: TFn, a: TerminalPaletteActions) {
  usePaletteActions('terminal', [
    {
      id: 'terminal.new',
      title: t('newSession'),
      hint: t('title'),
      icon: 'plus',
      hue: 'term',
      keywords: ['shell', 'terminal'],
      run: a.newLocal,
    },
    {
      id: 'terminal.root',
      title: t('newRoot'),
      hint: t('title'),
      icon: 'lock',
      hue: 'term',
      keywords: ['sudo', 'admin'],
      run: a.newRoot,
    },
    {
      id: 'terminal.focus',
      title: t('focusMode'),
      hint: t('title'),
      icon: 'overview',
      hue: 'term',
      run: a.toggleFocus,
    },
  ]);
}

interface PaletteInput extends TerminalPaletteActions {
  t: TFn;
  lang: string;
  snippets: Snippet[];
  sessions: SessionInfo[];
  history: HistoryItem[];
  mobile: boolean;
  split: boolean;
  focus: boolean;
  showSnippets: boolean;
  runCmd(cmd: string): void;
  pasteCmd(cmd: string): void;
  showSession(id: string): void;
  newSsh(): void;
  toggleSplit(): void;
  addSnippet(): void;
  setShowSnippets(v: boolean): void;
}

function commandItems(p: PaletteInput): PaletteItem[] {
  return [
    ...p.snippets.map((s) => {
      const look = snippetLook(s);
      return {
        id: 'sn' + s.id,
        group: 'snippets' as const,
        title: s.title,
        sub: s.command,
        icon: look.icon,
        hue: look.hue,
        run: () => p.runCmd(s.command),
        paste: () => p.pasteCmd(s.command),
      };
    }),
    ...p.sessions.map((s) => ({
      id: 'se' + s.id,
      group: 'sessions' as const,
      title: p.t('palette.openName', { name: s.name }),
      sub: p.t('palette.switch'),
      ...kindLook(s.kind),
      keywords: s.kind,
      run: () => p.showSession(s.id),
    })),
    ...p.history.map((h, i) => ({
      id: 'hi' + i,
      group: 'history' as const,
      title: h.cmd,
      sub: relativeTime(h.at, p.lang),
      icon: 'clock' as const,
      hue: 'sw',
      run: () => p.runCmd(h.cmd),
      paste: () => p.pasteCmd(h.cmd),
    })),
  ];
}

function actionItems(p: PaletteInput): PaletteItem[] {
  const { t } = p;
  return [
    {
      id: 'a-new',
      group: 'actions' as const,
      title: t('newSession'),
      icon: 'plus' as const,
      hue: 'term',
      run: p.newLocal,
    },
    {
      id: 'a-root',
      group: 'actions' as const,
      title: t('newRoot'),
      icon: 'lock' as const,
      hue: 'svc',
      keywords: 'sudo admin',
      run: p.newRoot,
    },
    {
      id: 'a-ssh',
      group: 'actions' as const,
      title: t('newSsh'),
      icon: 'server' as const,
      hue: 'ov',
      run: p.newSsh,
    },
    ...(p.mobile
      ? []
      : [
          {
            id: 'a-split',
            group: 'actions' as const,
            title: p.split ? t('splitOff') : t('splitOn'),
            icon: 'columns' as const,
            hue: 'term',
            run: p.toggleSplit,
          },
        ]),
    {
      id: 'a-focus',
      group: 'actions' as const,
      title: p.focus ? t('focusOff') : t('focusMode'),
      icon: 'overview' as const,
      hue: 'term',
      run: p.toggleFocus,
    },
    {
      id: 'a-save',
      group: 'actions' as const,
      title: t('snippet.add'),
      icon: 'command' as const,
      hue: 'term',
      run: p.addSnippet,
    },
    {
      id: 'a-chips',
      group: 'actions' as const,
      title: p.showSnippets ? t('hideSnippets') : t('showSnippets'),
      icon: 'eye' as const,
      hue: 'term',
      run: () => p.setShowSnippets(!p.showSnippets),
    },
  ];
}

// Items for the terminal's own palette: snippets, sessions, history, then actions.
export function terminalPaletteItems(p: PaletteInput): PaletteItem[] {
  return [...commandItems(p), ...actionItems(p)];
}
