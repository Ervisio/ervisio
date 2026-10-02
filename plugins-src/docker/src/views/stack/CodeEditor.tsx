import { useMemo, useRef, type KeyboardEvent } from 'react';
import { t } from '../../i18n';
import { highlightLine, type Lang } from './highlight';
import type { Issue } from './validate';

/**
 * A code editor made of a textarea laid over a highlighted copy of the text. Line numbers on the left; changed lines
 * get a green marker, lines with a problem a red or amber one. Tab inserts two spaces, Enter keeps the indent.
 * With `readOnly` it is only a viewer.
 */
export function CodeEditor({ value, onChange, lang, issues = [], changed, readOnly, label, minLines = 14 }: {
  value: string;
  onChange?(v: string): void;
  lang: Lang;
  issues?: Issue[];
  changed?: Set<number>;
  readOnly?: boolean;
  label: string;
  minLines?: number;
}) {
  const ta = useRef<HTMLTextAreaElement>(null);
  const lines = useMemo(() => value.split('\n'), [value]);
  const html = useMemo(() => lines.map((l) => highlightLine(l, lang) || '​'), [lines, lang]);
  const byLine = useMemo(() => {
    const m = new Map<number, Issue[]>();
    for (const i of issues) m.set(i.line, [...(m.get(i.line) ?? []), i]);
    return m;
  }, [issues]);

  const set = (v: string, selStart: number, selEnd = selStart) => {
    onChange?.(v);
    requestAnimationFrame(() => ta.current?.setSelectionRange(selStart, selEnd));
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (readOnly) return;
    const el = e.currentTarget;
    const { selectionStart: s, selectionEnd: en, value: v } = el;
    if (e.key === 'Tab' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      const ls = v.lastIndexOf('\n', s - 1) + 1;
      if (s !== en && v.slice(s, en).includes('\n')) {
        // Indent or outdent every selected line.
        const block = v.slice(ls, en);
        const out = e.shiftKey ? block.replace(/^( {1,2}|\t)/gm, '') : block.replace(/^/gm, '  ');
        set(v.slice(0, ls) + out + v.slice(en), ls, ls + out.length);
      } else if (e.shiftKey) {
        const m = /^( {1,2}|\t)/.exec(v.slice(ls));
        if (m) set(v.slice(0, ls) + v.slice(ls + m[0].length), Math.max(ls, s - m[0].length));
      } else set(v.slice(0, s) + '  ' + v.slice(en), s + 2);
    } else if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey) {
      e.preventDefault();
      const ls = v.lastIndexOf('\n', s - 1) + 1;
      const line = v.slice(ls, s);
      let indent = /^\s*/.exec(line)![0];
      if (lang === 'yaml' && /:\s*$/.test(line) && !/^\s*#/.test(line)) indent += '  ';
      const ins = '\n' + indent;
      set(v.slice(0, s) + ins + v.slice(en), s + ins.length);
    }
  };

  return (
    <div className="dk-ed" data-readonly={readOnly ? 'true' : undefined}>
      <div className="dk-ed-scroll" style={{ minHeight: `${minLines * 21 + 24}px` }}>
        <div className="dk-ed-gut" aria-hidden="true">
          {lines.map((_, i) => {
            const is = byLine.get(i + 1);
            const lvl = is?.some((x) => x.level === 'error') ? 'err' : is ? 'warn' : changed?.has(i + 1) ? 'chg' : '';
            return <span key={i} className={lvl ? `dk-ed-n dk-ed-n--${lvl}` : 'dk-ed-n'}>{i + 1}</span>;
          })}
        </div>
        <div className="dk-ed-body">
          <pre className="dk-ed-pre" aria-hidden="true">
            {html.map((h, i) => {
              const is = byLine.get(i + 1);
              const cls = is?.some((x) => x.level === 'error') ? ' dk-ed-l--err' : is ? ' dk-ed-l--warn' : changed?.has(i + 1) ? ' dk-ed-l--chg' : '';
              return <span key={i} className={`dk-ed-l${cls}`} dangerouslySetInnerHTML={{ __html: h }} />;
            })}
          </pre>
          <textarea
            ref={ta}
            className="dk-ed-ta"
            value={value}
            readOnly={readOnly}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            wrap="off"
            aria-label={label}
            title={readOnly ? t('stacks.readOnlyHint') : undefined}
            onChange={(e) => onChange?.(e.target.value)}
            onKeyDown={onKey}
          />
        </div>
      </div>
    </div>
  );
}

/** Put the caret at the start of a 1-based line and bring it into view. */
export function jumpToLine(root: HTMLElement | null, line: number): void {
  const ta = root?.querySelector('textarea');
  if (!ta) return;
  const pos = ta.value.split('\n').slice(0, line - 1).reduce((n, l) => n + l.length + 1, 0);
  ta.focus();
  ta.setSelectionRange(pos, pos);
  const scroller = ta.closest('.dk-ed-scroll') as HTMLElement | null;
  if (scroller) scroller.scrollTop = Math.max(0, (line - 4) * 21);
}
