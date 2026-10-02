import { useEffect, useRef, useState } from 'react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal, type ITheme } from '@xterm/xterm';
import { t } from '../../i18n';
import { Button, EmptyState, Select } from '../../kit';
import { getSdk } from '../../sdk';
import { resolveColour, usePrefs } from './util';

const SHELLS = ['/bin/sh', '/bin/bash', '/bin/ash', '/bin/zsh'];

function buildTheme(host: Element): ITheme {
  const c = (css: string, fb: string) => resolveColour(css, fb, host);
  const mix = (v: string, fb: string) => resolveColour(`color-mix(in srgb, ${v} 78%, #fff)`, fb, host);
  const bg = c('var(--bg)', '#000000');
  const fg = c('var(--ink)', '#f4f4f6');
  const red = c('var(--err)', '#ff6b81');
  const green = c('var(--ok)', '#3ddc97');
  const yellow = c('var(--warn)', '#ffb547');
  const blue = c('var(--info)', '#5ab0ff');
  const magenta = c('var(--h-sw)', '#b98cff');
  const cyan = c('var(--h-usr)', '#3fd8de');
  return {
    background: bg, foreground: fg, cursor: green, cursorAccent: bg,
    selectionBackground: resolveColour('color-mix(in srgb, var(--acc) 35%, var(--bg))', '#24304a', host),
    black: c('var(--sunk)', '#18181b'), red, green, yellow, blue, magenta, cyan, white: c('var(--ink2)', '#a3a3ad'),
    brightBlack: c('var(--ink3)', '#6c6c76'), brightRed: mix('var(--err)', red), brightGreen: mix('var(--ok)', green), brightYellow: mix('var(--warn)', yellow),
    brightBlue: mix('var(--info)', blue), brightMagenta: mix('var(--h-sw)', magenta), brightCyan: mix('var(--h-usr)', cyan), brightWhite: fg,
  };
}

type Status = 'idle' | 'connecting' | 'open' | 'ended' | 'error';

/** An interactive shell in the container through the manifest command `shell` (docker exec -it). */
export function ShellTab({ id, running, visible, onStart }: { id: string; running: boolean; visible: boolean; onStart(): void }) {
  const [prefs, setPrefs, ready] = usePrefs();
  const host = useRef<HTMLDivElement>(null);
  const [round, setRound] = useState(0);
  const [status, setStatus] = useState<Status>('idle');
  const [info, setInfo] = useState('');
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const sessionRef = useRef<{ resize(c: number, r: number): void } | null>(null);
  const shell = SHELLS.includes(prefs.shell) ? prefs.shell : '/bin/sh';

  useEffect(() => {
    const el = host.current;
    if (!el || !running || !ready) {
      setStatus('idle');
      return;
    }
    const term = new Terminal({
      fontFamily: '"JetBrains Mono", ui-monospace, "SFMono-Regular", Menlo, monospace',
      fontSize: 13,
      cursorBlink: true,
      scrollback: 5000,
      theme: buildTheme(el),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    termRef.current = term;
    fitRef.current = fit;
    const doFit = () => {
      if (el.clientWidth < 20 || el.clientHeight < 20) return;
      try {
        fit.fit();
      } catch {
        /* not laid out yet */
      }
    };
    doFit();
    setStatus('connecting');
    setInfo('');
    let ended = false;
    let session: { write(d: string | Uint8Array): void; resize(c: number, r: number): void; close(): void } | null = null;
    try {
      session = getSdk().api.pty('shell', [id, shell], {
        cols: term.cols || 80,
        rows: term.rows || 24,
        onData: (chunk) => {
          setStatus((s) => (s === 'connecting' ? 'open' : s));
          term.write(chunk);
        },
        onExit: (code) => {
          ended = true;
          setStatus('ended');
          setInfo(code === 126 || code === 127 ? t('container.shell.noShell', { shell }) : t('container.shell.exited', { code }));
          term.write(`\r\n\x1b[2m[${t('container.shell.exited', { code })}]\x1b[0m\r\n`);
        },
        onError: (e) => {
          if (ended) return;
          ended = true;
          setStatus('error');
          setInfo(e.message);
          term.write(`\r\n\x1b[31m${e.message}\x1b[0m\r\n`);
        },
      });
    } catch (e) {
      setStatus('error');
      setInfo((e as Error).message);
    }
    sessionRef.current = session;
    const data = term.onData((d) => {
      if (!ended) session?.write(d);
    });
    const resize = term.onResize(({ cols, rows }) => session?.resize(cols, rows));
    const ro = new ResizeObserver(doFit);
    ro.observe(el);
    return () => {
      ro.disconnect();
      data.dispose();
      resize.dispose();
      session?.close();
      sessionRef.current = null;
      term.dispose();
      termRef.current = null;
    };
  }, [id, running, ready, shell, round]);

  useEffect(() => {
    if (visible) {
      const i = setTimeout(() => {
        try {
          fitRef.current?.fit();
        } catch {
          /* hidden */
        }
        termRef.current?.focus();
      }, 30);
      return () => clearTimeout(i);
    }
  }, [visible]);

  return (
    <div className="dk-card dk-c-shell">
      <div className="dk-c-shbar">
        <span className="dk-muted">{t('container.shell.run')}</span>
        <span className="dk-c-sel dk-c-sel--sm"><Select compact options={SHELLS.map((s) => ({ value: s, label: s }))} value={shell} onChange={(v) => setPrefs({ shell: v })} /></span>
        <span className={`dk-c-live${status === 'open' ? '' : ' dk-c-live--off'}`}><i />{t(`container.shell.s.${status}`)}</span>
        <Button size="sm" icon="refresh" onClick={() => setRound((n) => n + 1)} disabled={!running}>{t('container.shell.reconnect')}</Button>
      </div>
      {!running && (
        <EmptyState
          icon="terminal"
          hue="term"
          title={t('container.shell.stopped.title')}
          text={t('container.shell.stopped.text')}
          action={<Button variant="primary" icon="play" onClick={onStart}>{t('common.start')}</Button>}
        />
      )}
      <div className="dk-c-term" ref={host} hidden={!running} />
      {info && running && <p className="dk-muted dk-c-shell-info">{info}</p>}
    </div>
  );
}
