import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { docker, errorText } from '../../api/engine';
import { LogLines, type LogLine } from '../../api/streams';
import { t } from '../../i18n';
import { Button, Icon, IconButton, Input, Segmented, Select, toast } from '../../kit';
import { getSdk } from '../../sdk';
import { CONFIG_DIR } from '../../settings';
import { copyText, escapeRe, usePrefs } from './util';

const BUFFER = 20000;
const ROWS = 1500;
const FLUSH_MS = 120;

type Level = 'err' | 'warn' | '';
const levelOf = (l: LogLine): Level => {
  if (/\b(fatal|panic|critical|error|err)\b/i.test(l.text)) return 'err';
  if (/\b(warn|warning)\b/i.test(l.text)) return 'warn';
  return l.stream === 'stderr' ? 'err' : '';
};

const clock = (ts: string): string => {
  const d = new Date(ts);
  if (!isFinite(d.getTime())) return ts;
  const p = (n: number, w = 2) => String(n).padStart(w, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
};

function highlight(text: string, re: RegExp | null) {
  if (!re) return text;
  const out: React.ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  re.lastIndex = 0;
  while ((m = re.exec(text))) {
    if (!m[0]) { re.lastIndex++; continue; }
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push(<mark key={m.index}>{m[0]}</mark>);
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/** Live logs: follows the stream, searches, pauses, and stops following when the user scrolls up. */
export function LogsTab({ id, name, tty, running }: { id: string; name: string; tty: boolean; running: boolean }) {
  const [prefs, setPrefs] = usePrefs();
  const [q, setQ] = useState('');
  const [since, setSince] = useState('0');
  const [paused, setPaused] = useState(false);
  const [snap, setSnap] = useState<LogLine[]>([]);
  const [status, setStatus] = useState<'connecting' | 'live' | 'ended' | 'error'>('connecting');
  const [error, setError] = useState('');
  const [round, setRound] = useState(0);
  const [, setVersion] = useState(0);
  const [atBottom, setAtBottom] = useState(true);

  const buf = useRef<LogLine[]>([]);
  const dirty = useRef(false);
  const stick = useRef(true);
  const box = useRef<HTMLDivElement>(null);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  const { tail, wrap, timestamps } = prefs;

  useEffect(() => {
    buf.current = [];
    dirty.current = false;
    stick.current = true;
    setStatus('connecting');
    setError('');
    setVersion((n) => n + 1);
    const query: Record<string, string> = { follow: '1', stdout: '1', stderr: '1', timestamps: '1', tail };
    if (since !== '0') query.since = String(Math.floor(Date.now() / 1000) - Number(since));
    const lines = new LogLines(tty, true, (l) => {
      buf.current.push(l);
      if (buf.current.length > BUFFER) buf.current.splice(0, buf.current.length - BUFFER);
      dirty.current = true;
    });
    const timer = setInterval(() => {
      if (dirty.current) {
        dirty.current = false;
        setVersion((n) => n + 1);
      }
    }, FLUSH_MS);
    const h = docker.stream('GET', `/containers/${encodeURIComponent(id)}/logs`, { query }, {
      onStart: () => setStatus('live'),
      onData: (c) => lines.push(c),
      onEnd: () => {
        lines.flush();
        dirty.current = true;
        setStatus('ended');
      },
      onError: (e) => {
        setError(errorText(e));
        setStatus('error');
      },
    });
    return () => {
      clearInterval(timer);
      h.close();
    };
  }, [id, tty, tail, since, round]);

  const needle = q.trim();
  const re = useMemo(() => (needle ? new RegExp(escapeRe(needle), 'gi') : null), [needle]);
  const source = paused ? snap : buf.current;
  const matched = needle ? source.filter((l) => l.text.toLowerCase().includes(needle.toLowerCase())) : source;
  const rows = matched.length > ROWS ? matched.slice(matched.length - ROWS) : matched;
  const fresh = paused ? Math.max(0, buf.current.length - snap.length) : 0;

  useLayoutEffect(() => {
    const el = box.current;
    if (el && stick.current && !paused) el.scrollTop = el.scrollHeight;
  });

  const onScroll = () => {
    const el = box.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 32;
    stick.current = near;
    setAtBottom((a) => (a === near ? a : near));
  };
  const toLatest = () => {
    stick.current = true;
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
    setAtBottom(true);
  };
  const togglePause = () => {
    if (!paused) setSnap(buf.current.slice());
    else stick.current = true;
    setPaused(!paused);
  };

  const asText = () => matched.map((l) => (timestamps && l.ts ? `${l.ts} ${l.text}` : l.text)).join('\n') + '\n';
  const copy = async () => {
    if (await copyText(asText())) toast.ok(t('container.logs.copied', { n: matched.length }));
    else toast.err(t('container.copyFail'));
  };
  /** The plugin frame cannot start a browser download, so the log is written to a file in the plugin's folder. */
  const save = async () => {
    const file = `${CONFIG_DIR}/logs/${name.replace(/[^a-zA-Z0-9_.-]/g, '_')}.log`;
    try {
      await getSdk().files.mkdir(`${CONFIG_DIR}/logs`).catch(() => undefined);
      await getSdk().files.write(file, asText());
      toast.ok(t('container.logs.saved'), file);
    } catch (e) {
      toast.err(t('container.logs.saveFail'), errorText(e));
    }
  };

  const live = status === 'live' && running && !paused;
  return (
    <div className="dk-card dk-c-logs">
      <div className="dk-c-lgbar">
        <div className="dk-c-lg-search">
          <Input compact icon="search" placeholder={t('container.logs.search')} aria-label={t('container.logs.search')} value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <span className={`dk-c-live${live ? '' : ' dk-c-live--off'}`}><i />{paused ? t('container.logs.paused') : live ? t('container.logs.live') : status === 'error' ? t('container.logs.error') : status === 'connecting' ? t('container.logs.connecting') : t('container.logs.ended')}</span>
        <span className="dk-c-sel"><Select compact options={['100', '500', '1000', '5000', 'all'].map((v) => ({ value: v, label: v === 'all' ? t('container.logs.tailAll') : t('container.logs.tail', { n: v }) }))} value={tail} onChange={(v) => setPrefs({ tail: v })} /></span>
        <span className="dk-c-sel"><Select compact options={[['0', 'sinceAll'], ['300', 'since5m'], ['3600', 'since1h'], ['86400', 'since24h']].map(([v, k]) => ({ value: v, label: t(`container.logs.${k}`) }))} value={since} onChange={setSince} /></span>
        <Button size="sm" icon={paused ? 'play' : 'pause'} onClick={togglePause}>{paused ? t('container.logs.resume') : t('container.logs.pause')}</Button>
        <Segmented
          aria-label={t('container.logs.view')}
          options={[{ value: 'wrap', icon: 'list', title: t('container.logs.wrap') }, { value: 'nowrap', icon: 'columns', title: t('container.logs.nowrap') }]}
          value={wrap ? 'wrap' : 'nowrap'}
          onChange={(v) => setPrefs({ wrap: v === 'wrap' })}
        />
        <Button size="sm" variant={timestamps ? 'secondary' : 'ghost'} icon="clock" onClick={() => setPrefs({ timestamps: !timestamps })}>{t('container.logs.time')}</Button>
        <IconButton icon="copy" label={t('container.logs.copy')} onClick={copy} />
        <IconButton icon="download" label={t('container.logs.save')} onClick={save} />
      </div>
      <div className="dk-c-lgwrap">
        <div ref={box} className={`dk-c-lgbox${wrap ? '' : ' dk-c-lgbox--nowrap'}`} onScroll={onScroll} role="log" aria-live="off" tabIndex={0}>
          {rows.map((l, i) => {
            const lv = levelOf(l);
            return (
              <div key={`${l.ts}-${i}`} className={`dk-c-lg${lv ? ` dk-c-lg--${lv}` : ''}`}>
                {timestamps && l.ts && <span className="dk-c-lg-ts">{clock(l.ts)}</span>}
                <span className="dk-c-lg-msg">{highlight(l.text, re)}</span>
              </div>
            );
          })}
          {rows.length === 0 && (
            <p className="dk-c-lg-empty">{status === 'error' ? error : status === 'connecting' ? t('container.logs.connecting') : needle ? t('container.logs.noMatch') : t('container.logs.empty')}</p>
          )}
        </div>
        {!atBottom && !paused && <Button className="dk-c-lg-jump" size="sm" variant="primary" icon="chevron" onClick={toLatest}>{t('container.logs.latest')}</Button>}
      </div>
      <div className="dk-c-lgfoot">
        <span className="dk-muted">{needle ? t('container.logs.matches', { n: matched.length, total: source.length }) : t('container.logs.count', { n: source.length })}{rows.length < matched.length ? ` ${t('container.logs.showingLast', { n: rows.length })}` : ''}</span>
        {paused && fresh > 0 && <span className="dk-muted">{t('container.logs.newLines', { n: fresh })}</span>}
        {status === 'error' && <Button size="sm" icon="refresh" onClick={() => setRound((n) => n + 1)}>{t('common.retry')}</Button>}
        {status === 'ended' && running && <Button size="sm" icon="refresh" onClick={() => setRound((n) => n + 1)}>{t('container.logs.follow')}</Button>}
        <span className="dk-c-lg-key"><Icon name="info" />{t('container.logs.stderrNote')}</span>
      </div>
    </div>
  );
}
