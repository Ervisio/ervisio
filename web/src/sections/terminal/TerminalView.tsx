import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
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
  const [status, setStatus] = useState<'ok' | 'reconnecting' | 'exited' | 'gone'>('ok');
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
      const code = { up: 'A', down: 'B', right: 'C', left: 'D' }[dir];
      sendRaw((app ? '\x1bO' : '\x1b[') + code);
      termRef.current?.focus();
    },
    focus: () => termRef.current?.focus(),
    copySelection: () => {
      const s = termRef.current?.getSelection();
      if (s) void navigator.clipboard?.writeText(s).catch(() => undefined);
    },
  }));

  // Terminal lifetime = one session id.
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const term = new Terminal({
      fontFamily: '"JetBrains Mono", ui-monospace, "SFMono-Regular", Menlo, monospace',
      fontSize: live.current.fontSize,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
      theme: buildTheme(live.current.themeMode),
      macOptionIsMeta: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    termRef.current = term;
    fitRef.current = fit;

    let closed = false;
    let first = true;
    let ended = false;
    let retry = 0;
    let timer: number | undefined;

    const connect = () => {
      if (closed) return;
      if (!first) term.reset();
      first = false;
      const h = stream<any>(
        'terminal.attach',
        { id, cols: term.cols, rows: term.rows },
        {
          admin,
          onData: (d) => {
            retry = 0;
            setStatus((s) => (s === 'reconnecting' ? 'ok' : s));
            if (d instanceof Uint8Array) {
              term.write(d);
              gotData.current = true;
              const init = live.current.initialInput;
              if (init) {
                live.current.onInitialInputUsed?.();
                // after the prompt has been drawn
                window.setTimeout(() => {
                  if (!closed) {
                    term.paste(init);
                    term.focus();
                  }
                }, 120);
              }
            }
            else if (d && d.type === 'exit') {
              ended = true;
              setExitCode(d.code ?? 0);
              setStatus('exited');
              term.write(`\r\n\x1b[2m[${t('exited', { code: d.code ?? 0 })}]\x1b[0m\r\n`);
              live.current.onEnded();
            }
          },
          onEnd: () => {
            if (!ended) schedule();
          },
          onError: (e) => {
            handleRef.current = null;
            if (closed || ended) return;
            if (e.code === 'not_found') {
              ended = true;
              setStatus('gone');
              live.current.onEnded();
              return;
            }
            schedule();
          },
        },
      );
      handleRef.current = h;
    };
    const schedule = () => {
      if (closed || ended) return;
      handleRef.current = null;
      setStatus('reconnecting');
      timer = window.setTimeout(connect, Math.min(500 * 2 ** retry, 8000));
      retry++;
    };

    const doFit = () => {
      if (el.clientWidth < 20 || el.clientHeight < 20) return;
      try {
        fit.fit();
      } catch {
        /* not laid out yet */
      }
    };
    doFit();
    connect();

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
    term.onBinary((d) => handleRef.current?.send({ type: 'input', data: toBase64(Uint8Array.from(d, (c) => c.charCodeAt(0))) }));
    term.onResize(({ cols, rows }) => handleRef.current?.send({ type: 'resize', cols, rows }));
    term.onSelectionChange(() => {
      if (live.current.copyOnSelect && term.hasSelection()) void navigator.clipboard?.writeText(term.getSelection()).catch(() => undefined);
    });
    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown' || !e.ctrlKey || !e.shiftKey) return true;
      if (e.code === 'KeyP') return false; // page palette
      if (e.code === 'KeyC') {
        const s = term.getSelection();
        if (s) void navigator.clipboard?.writeText(s).catch(() => undefined);
        return false;
      }
      if (e.code === 'KeyV') return false; // the browser pastes into the helper textarea
      return true;
    });

    let raf = 0;
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(doFit);
    });
    ro.observe(el);
    void document.fonts?.load('13px "JetBrains Mono"').then(doFit, () => undefined);
    const onFocusIn = () => live.current.onActivate();
    el.addEventListener('focusin', onFocusIn);
    el.addEventListener('mousedown', onFocusIn);

    return () => {
      closed = true;
      window.clearTimeout(timer);
      cancelAnimationFrame(raf);
      ro.disconnect();
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
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'data-scheme', 'class'] });
    return () => mo.disconnect();
  }, []);
  useEffect(() => {
    const term = termRef.current;
    if (term) term.options.theme = buildTheme(props.themeMode);
  }, [props.themeMode, tick]);
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    term.options.fontSize = props.fontSize;
    try {
      fitRef.current?.fit();
    } catch {
      /* ignore */
    }
  }, [props.fontSize]);

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
