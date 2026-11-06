import type { MutableRefObject } from 'react';
import { useT } from '../../i18n';
import { Button, EmptyState, Icon } from '../../ui';
import TerminalView from './TerminalView';
import type { SessionInfo, TermHandle } from './types';
import type { Panes } from './useTerminalPanes';

function prettyTitle(s: SessionInfo | undefined, home: string | undefined) {
  const raw = s?.title || s?.name || '';
  return home && home !== '/' ? raw.split(home).join('~') : raw;
}

function EmptyPane({
  isActive,
  onActivate,
  onNew,
  onClose,
}: {
  isActive: boolean;
  onActivate(): void;
  onNew(): void;
  onClose(): void;
}) {
  const t = useT('terminal');
  return (
    <div className="terminal-pane" onMouseDown={onActivate}>
      <div className={`terminal-pane-h${isActive ? ' act' : ''}`}>
        <span className="terminal-pane-t">{t('emptyPane')}</span>
        <span className="sp" />
        <button type="button" aria-label={t('closePane')} title={t('closePane')} onClick={onClose}>
          <Icon name="close" />
        </button>
      </div>
      <div className="terminal-pane-empty">
        <Button icon="plus" onClick={onNew}>
          {t('newSession')}
        </Button>
      </div>
    </div>
  );
}

export interface TerminalPaneShared {
  home?: string;
  fontSize: number;
  fontFamily?: string;
  transparent?: boolean;
  cursorStyle?: 'block' | 'bar' | 'underline';
  cursorBlink?: boolean;
  lineHeight?: number;
  scrollback?: number;
  copyOnSelect: boolean;
  themeMode: string;
  ctrl: boolean;
  pendingInput: { id: string; text: string } | null;
  handles: MutableRefObject<[TermHandle | null, TermHandle | null]>;
  infoOf(id: string | null): SessionInfo | undefined;
  onCtrlUsed(): void;
  onEnded(): void;
  onInitialInputUsed(): void;
  onActivate(idx: 0 | 1): void;
  onClosePane(idx: 0 | 1): void;
  onNew(): void;
}

function TerminalPaneSlot({
  idx,
  id,
  isActive,
  shared: s,
}: {
  idx: 0 | 1;
  id: string | null;
  isActive: boolean;
  shared: TerminalPaneShared;
}) {
  const t = useT('terminal');
  if (!id) {
    return (
      <EmptyPane
        onClose={() => s.onClosePane(idx)}
        isActive={isActive}
        onActivate={() => s.onActivate(idx)}
        onNew={() => {
          s.onActivate(idx);
          s.onNew();
        }}
      />
    );
  }
  const info = s.infoOf(id);
  return (
    <div className="terminal-pane">
      <div className={`terminal-pane-h${isActive ? ' act' : ''}`}>
        <span className="terminal-pane-t">{prettyTitle(info, s.home)}</span>
        <span className="sp" />
        <button
          type="button"
          aria-label={t('closePane')}
          title={t('closePane')}
          onClick={() => s.onClosePane(idx)}
        >
          <Icon name="close" />
        </button>
      </div>
      <TerminalView
        ref={(h) => {
          s.handles.current[idx] = h;
        }}
        id={id}
        admin={!!info?.admin}
        fontSize={s.fontSize}
        fontFamily={s.fontFamily}
        transparent={s.transparent}
        cursorStyle={s.cursorStyle}
        cursorBlink={s.cursorBlink}
        lineHeight={s.lineHeight}
        scrollback={s.scrollback}
        copyOnSelect={s.copyOnSelect}
        themeMode={s.themeMode}
        ctrl={s.ctrl && isActive}
        onCtrlUsed={s.onCtrlUsed}
        onActivate={() => s.onActivate(idx)}
        onEnded={s.onEnded}
        onClosePane={() => s.onClosePane(idx)}
        initialInput={s.pendingInput?.id === id ? s.pendingInput.text : undefined}
        onInitialInputUsed={s.onInitialInputUsed}
      />
    </div>
  );
}

interface PaneAreaProps {
  loadError: string;
  hasSessions: boolean;
  showNone: boolean;
  visible: Panes;
  splitShown: boolean;
  activePane: 0 | 1;
  onRetry(): void;
  pane: TerminalPaneShared;
}

// The load error, the "no sessions" prompt, or the one or two terminal panes.
export function TerminalPaneArea(p: PaneAreaProps) {
  const t = useT('terminal');
  if (p.loadError && !p.hasSessions) {
    return (
      <div className="terminal-center">
        <EmptyState
          icon="alert"
          hue="term"
          title={t('loadFailed')}
          text={p.loadError}
          action={<Button onClick={p.onRetry}>{t('retry')}</Button>}
        />
      </div>
    );
  }
  if (!p.visible[0] && !p.visible[1]) {
    return (
      <div className="terminal-center">
        {p.showNone ? (
          <EmptyState
            icon="terminal"
            hue="term"
            title={t('noneTitle')}
            text={t('noneText')}
            action={
              <Button variant="primary" icon="plus" onClick={p.pane.onNew}>
                {t('newSession')}
              </Button>
            }
          />
        ) : null}
      </div>
    );
  }
  const slot = (idx: 0 | 1) => (
    <TerminalPaneSlot
      key={p.visible[idx] ?? `empty${idx}`}
      idx={idx}
      id={p.visible[idx]}
      isActive={p.activePane === idx}
      shared={p.pane}
    />
  );
  return (
    <div className="terminal-panes">
      {slot(0)}
      {p.splitShown && slot(1)}
    </div>
  );
}
