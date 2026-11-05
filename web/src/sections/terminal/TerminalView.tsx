import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type Dispatch,
  type MutableRefObject,
  type SetStateAction,
} from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { stream, toBase64, type StreamHandle } from '../../api';
import { useT } from '../../i18n';
import { Button } from '../../ui';
import { buildTheme } from './xtermTheme';
import type { TermHandle } from './types';

interface Props {
  id: string;
  admin: boolean;
  fontSize: number;
  fontFamily?: string;
  transparent?: boolean;
  cursorStyle?: 'block' | 'bar' | 'underline';
  cursorBlink?: boolean;
  lineHeight?: number;
  scrollback?: number;
  copyOnSelect: boolean;
  /** terminal.theme preference: app | dark | light */
  themeMode: string;
  /** Mobile Ctrl latch: the next typed letter becomes a control character. */
  ctrl: boolean;
  onCtrlUsed(): void;
  onActivate(): void;
  /** The session ended (shell exited) or no longer exists. */
  onEnded(): void;
  onClosePane(): void;
  /** Typed at the prompt (not run) once the session shows its first output. */
  initialInput?: string;
  onInitialInputUsed?(): void;
}

const enc = new TextEncoder();

/** Turns "c" into Ctrl+C etc. Returns null when the character has no control form. */
export function toControl(ch: string): string | null {
  if (ch.length !== 1) return null;
  const c = ch.toUpperCase().charCodeAt(0);
  if (c >= 64 && c <= 95) return String.fromCharCode(c - 64);
  if (ch === ' ') return '\x00';
  if (ch === '?') return '\x7f';
  return null;
}

type Status = 'ok' | 'reconnecting' | 'exited' | 'gone';
type Translate = ReturnType<typeof useT>;

const ARROW_CODES = { up: 'A', down: 'B', right: 'C', left: 'D' } as const;
const PROMPT_DRAW_DELAY_MS = 120;
const RETRY_BASE_MS = 500;
const RETRY_MAX_MS = 8000;

function copySilently(text: string) {
  void navigator.clipboard?.writeText(text).catch(() => undefined);
}

function createTerminal(el: HTMLElement, options: Props) {
  const term = new Terminal({
    fontFamily: options.fontFamily ?? '"JetBrains Mono", ui-monospace, "SFMono-Regular", Menlo, monospace',
    fontSize: options.fontSize,
    cursorBlink: options.cursorBlink ?? true,
    cursorStyle: options.cursorStyle ?? 'block',
    lineHeight: options.lineHeight ?? 1,
    scrollback: options.scrollback ?? 5000,
    allowTransparency: !!options.transparent,
    allowProposedApi: true,
    theme: { ...buildTheme(options.themeMode), ...(options.transparent ? { background: '#00000000' } : {}) },
    macOptionIsMeta: true,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el);
  return { term, fit };
}

function fitToHost(el: HTMLElement, fit: FitAddon) {
  if (el.clientWidth < 20 || el.clientHeight < 20) return;
  try {
    fit.fit();
  } catch {
    /* not laid out yet */
  }
}

interface AttachOptions {
  term: Terminal;
  id: string;
  admin: boolean;
  t: Translate;
  live: MutableRefObject<Props>;
  gotData: MutableRefObject<boolean>;
  handleRef: MutableRefObject<StreamHandle | null>;
  setStatus: Dispatch<SetStateAction<Status>>;
  setExitCode(code: number): void;
}

function typeInitialInput(o: AttachOptions, isClosed: () => boolean) {
  const { term, live } = o;
  const init = live.current.initialInput;
  if (!init) return;
  live.current.onInitialInputUsed?.();
  // after the prompt has been drawn
  window.setTimeout(() => {
    if (!isClosed()) {
      term.paste(init);
      term.focus();
    }
  }, PROMPT_DRAW_DELAY_MS);
}

function announceExit(o: AttachOptions, code: number) {
  o.setExitCode(code);
  o.setStatus('exited');
  o.term.write(`\r\n\x1b[2m[${o.t('exited', { code })}]\x1b[0m\r\n`);
  o.live.current.onEnded();
}

/** Attaches to the session and re-attaches with backoff until it ends. Returns the stop function. */
function attachSession(o: AttachOptions): () => void {
  const { term, live, handleRef } = o;
  let closed = false;
  let first = true;
  let ended = false;
  let retry = 0;
  let timer: number | undefined;

  const onData = (d: any) => {
    retry = 0;
    o.setStatus((s) => (s === 'reconnecting' ? 'ok' : s));
    if (d instanceof Uint8Array) {
      term.write(d);
      o.gotData.current = true;
      typeInitialInput(o, () => closed);
    } else if (d && d.type === 'exit') {
      ended = true;
      announceExit(o, d.code ?? 0);
    }
  };

  const onError = (e: { code?: string }) => {
    handleRef.current = null;
    if (closed || ended) return;
    if (e.code !== 'not_found') {
      schedule();
      return;
    }
    ended = true;
    o.setStatus('gone');
    live.current.onEnded();
  };

  const connect = () => {
    if (closed) return;
    if (!first) term.reset();
    first = false;
    handleRef.current = stream<any>(
      'terminal.attach',
      { id: o.id, cols: term.cols, rows: term.rows },
      {
        admin: o.admin,
        onData,
        onEnd: () => {
          if (!ended) schedule();
        },
        onError,
      },
    );
  };
  const schedule = () => {
    if (closed || ended) return;
    handleRef.current = null;
    o.setStatus('reconnecting');
    timer = window.setTimeout(connect, Math.min(RETRY_BASE_MS * 2 ** retry, RETRY_MAX_MS));
    retry++;
  };

  connect();
  return () => {
    closed = true;
    window.clearTimeout(timer);
  };
}

function bindInput(term: Terminal, live: MutableRefObject<Props>, sendRaw: (s: string) => void) {
  term.onData((d) => {
    let out = d;
    if (live.current.ctrl && d.length === 1) {
      const c = toControl(d);
      if (c !== null) {
        out = c;
        live.current.onCtrlUsed();
      }
    }
    sendRaw(out);
  });
}

function bindShortcutKeys(term: Terminal) {
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown' || !e.ctrlKey || !e.shiftKey) return true;
    if (e.code === 'KeyP') return false; // page palette
    if (e.code === 'KeyC') {
      const s = term.getSelection();
      if (s) copySilently(s);
      return false;
    }
    if (e.code === 'KeyV') return false; // the browser pastes into the helper textarea
    return true;
  });
}

/** Wires keyboard, paste, resize and selection events of the terminal to the session stream. */
function bindTerminalEvents(
  term: Terminal,
  live: MutableRefObject<Props>,
  handleRef: MutableRefObject<StreamHandle | null>,
  sendRaw: (s: string) => void,
) {
  bindInput(term, live, sendRaw);
  term.onBinary((d) =>
    handleRef.current?.send({
      type: 'input',
      data: toBase64(Uint8Array.from(d, (c) => c.charCodeAt(0))),
    }),
  );
  term.onResize(({ cols, rows }) => handleRef.current?.send({ type: 'resize', cols, rows }));
  term.onSelectionChange(() => {
    if (live.current.copyOnSelect && term.hasSelection()) copySilently(term.getSelection());
  });
  bindShortcutKeys(term);
}

/** Refits on host resize (once per frame) and after the web font loads. Returns the stop function. */
function observeHostSize(el: HTMLElement, doFit: () => void): () => void {
  let raf = 0;
  const ro = new ResizeObserver(() => {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(doFit);
  });
  ro.observe(el);
  void document.fonts?.load('13px "JetBrains Mono"').then(doFit, () => undefined);
  return () => {
    cancelAnimationFrame(raf);
    ro.disconnect();
  };
}

/** One xterm bound to a persistent session: attaches (replaying the scrollback), resizes and reconnects. */
const TerminalView = forwardRef<TermHandle, Props>(function TerminalView(props, ref) {
  const t = useT('terminal');
  const { id, admin } = props;
  const host = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const handleRef = useRef<StreamHandle | null>(null);
  const live = useRef(props);
  live.current = props;
  const [status, setStatus] = useState<Status>('ok');
  const [exitCode, setExitCode] = useState(0);
  const [tick, setTick] = useState(0);
  const gotData = useRef(false);
  // initialInput set after the first output arrived: type it now.
  const { initialInput } = props;
  useEffect(() => {
    if (!initialInput || !gotData.current || !termRef.current) return;
    live.current.onInitialInputUsed?.();
    termRef.current.paste(initialInput);
    termRef.current.focus();
  }, [initialInput]);

  const sendRaw = (s: string) => {
    if (!s) return;
    handleRef.current?.send({ type: 'input', data: toBase64(enc.encode(s)) });
  };

  useImperativeHandle(ref, () => ({
    paste: (text) => {
      termRef.current?.paste(text);
      termRef.current?.focus();
    },
    run: (cmd) => {
      termRef.current?.paste(cmd);
      sendRaw('\r');
      termRef.current?.focus();
    },
    sendKeys: (seq) => {
      sendRaw(seq);
      termRef.current?.focus();
    },
    arrow: (dir) => {
      const app = termRef.current?.modes.applicationCursorKeysMode;
      sendRaw((app ? '\x1bO' : '\x1b[') + ARROW_CODES[dir]);
      termRef.current?.focus();
    },
    focus: () => termRef.current?.focus(),
    copySelection: () => {
      const s = termRef.current?.getSelection();
      if (s) copySilently(s);
    },
  }));

  // Terminal lifetime = one session id.
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const { term, fit } = createTerminal(el, live.current);
    termRef.current = term;
    fitRef.current = fit;

    const doFit = () => fitToHost(el, fit);
    doFit();
    const stopAttach = attachSession({
      term,
      id,
      admin,
      t,
      live,
      gotData,
      handleRef,
      setStatus,
      setExitCode,
    });
    bindTerminalEvents(term, live, handleRef, sendRaw);
    const stopObserving = observeHostSize(el, doFit);
    const onFocusIn = () => live.current.onActivate();
    el.addEventListener('focusin', onFocusIn);
    el.addEventListener('mousedown', onFocusIn);

    return () => {
      stopAttach();
      stopObserving();
      el.removeEventListener('focusin', onFocusIn);
      el.removeEventListener('mousedown', onFocusIn);
      handleRef.current?.close();
      handleRef.current = null;
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, admin]);

  // Follow the app theme (CSS variables on :root change when the theme or colour mode does).
  useEffect(() => {
    const mo = new MutationObserver(() => setTick((n) => n + 1));
    mo.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['style', 'data-scheme', 'class'],
    });
    return () => mo.disconnect();
  }, []);
  useEffect(() => {
    const term = termRef.current;
    if (term) term.options.theme = { ...buildTheme(props.themeMode), ...(props.transparent ? { background: '#00000000' } : {}) };
  }, [props.themeMode, props.transparent, tick]);
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    term.options.fontSize = props.fontSize;
    term.options.fontFamily = props.fontFamily ?? '"JetBrains Mono", ui-monospace, "SFMono-Regular", Menlo, monospace';
    term.options.cursorStyle = props.cursorStyle ?? 'block';
    term.options.cursorBlink = props.cursorBlink ?? true;
    term.options.lineHeight = props.lineHeight ?? 1;
    term.options.scrollback = props.scrollback ?? 5000;
    try {
      fitRef.current?.fit();
    } catch {
      /* ignore */
    }
  }, [props.fontSize, props.fontFamily, props.cursorStyle, props.cursorBlink, props.lineHeight, props.scrollback]);

  return (
    <div className="terminal-view">
      <div ref={host} className="terminal-host" />
      {status === 'reconnecting' && (
        <div className="terminal-banner" role="status">
          {t('reconnecting')}
        </div>
      )}
      {(status === 'exited' || status === 'gone') && (
        <div className="terminal-ended" role="status">
          <span>{status === 'gone' ? t('gone') : t('exitedBar', { code: exitCode })}</span>
          <Button size="sm" variant="secondary" onClick={props.onClosePane}>
            {t('closePane')}
          </Button>
        </div>
      )}
    </div>
  );
});

export default TerminalView;
