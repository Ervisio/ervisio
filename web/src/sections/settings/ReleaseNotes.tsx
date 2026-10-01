import type { ReactNode } from 'react';

/*
 * Release notes come from GitHub as Markdown. They are rendered as plain structured text with React elements:
 * headings, paragraphs, lists, quotes, code, bold/italic. HTML in the source is shown as text (React escapes it),
 * links keep only their text (no clickable URLs from a remote document), images become their alt text.
 */

type Block =
  | { k: 'h'; level: number; text: string }
  | { k: 'p'; text: string }
  | { k: 'ul' | 'ol'; items: string[] }
  | { k: 'quote'; text: string }
  | { k: 'code'; text: string };

const MAX_LINES = 400;

export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/<!--[\s\S]*?-->/g, '').replace(/\r\n?/g, '\n').split('\n').slice(0, MAX_LINES);
  const out: Block[] = [];
  let para: string[] = [];
  const flush = () => {
    if (para.length) out.push({ k: 'p', text: para.join(' ') });
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const t = line.trim();
    if (/^(```|~~~)/.test(t)) {
      flush();
      const fence = t.slice(0, 3);
      const code: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith(fence); i++) code.push(lines[i]);
      out.push({ k: 'code', text: code.join('\n') });
      continue;
    }
    if (!t) {
      flush();
      continue;
    }
    const h = /^(#{1,6})\s+(.*?)\s*#*$/.exec(t);
    if (h) {
      flush();
      out.push({ k: 'h', level: h[1].length, text: h[2] });
      continue;
    }
    if (/^([-*_])(\s*\1){2,}$/.test(t)) {
      flush(); // horizontal rule: just a break, no line
      continue;
    }
    if (/^>/.test(t)) {
      flush();
      const q: string[] = [];
      for (; i < lines.length && /^\s*>/.test(lines[i]); i++) q.push(lines[i].replace(/^\s*>\s?/, ''));
      i--;
      out.push({ k: 'quote', text: q.join(' ') });
      continue;
    }
    const li = /^\s*([-*+]|\d{1,3}[.)])\s+(.*)$/.exec(line);
    if (li) {
      flush();
      const kind = /\d/.test(li[1]) ? 'ol' : 'ul';
      const items: string[] = [];
      for (; i < lines.length; i++) {
        const m = /^\s*([-*+]|\d{1,3}[.)])\s+(.*)$/.exec(lines[i]);
        if (m) items.push(m[2].replace(/^\[[ xX]\]\s+/, ''));
        else if (lines[i].trim() && /^\s{2,}/.test(lines[i]) && items.length) items[items.length - 1] += ' ' + lines[i].trim();
        else break;
      }
      i--;
      out.push({ k: kind, items });
      continue;
    }
    para.push(t);
  }
  flush();
  return out;
}

/** Inline Markdown: `code`, **bold**, *italic* / _italic_, [text](url) -> text, ![alt](src) -> alt. */
export function inline(text: string, key = 'i'): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /(`+)([^`]+?)\1|\*\*([^*]+)\*\*|__([^_]+)__|\*([^*\s][^*]*)\*|(?<![\w])_([^_\s][^_]*)_(?![\w])|!?\[([^\]]*)\]\([^)]*\)|<(https?:\/\/[^>\s]+)>/g;
  let last = 0;
  let n = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const k = `${key}-${n++}`;
    if (m[2] !== undefined) out.push(<code key={k}>{m[2]}</code>);
    else if (m[3] !== undefined || m[4] !== undefined) out.push(<strong key={k}>{inline(m[3] ?? m[4], k)}</strong>);
    else if (m[5] !== undefined || m[6] !== undefined) out.push(<em key={k}>{inline(m[5] ?? m[6], k)}</em>);
    else if (m[7] !== undefined) out.push(<span key={k}>{inline(m[7], k)}</span>);
    else if (m[8] !== undefined) out.push(m[8]);
    last = re.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export function ReleaseNotes({ source, empty }: { source: string; empty?: ReactNode }) {
  const blocks = parseMarkdown(source);
  if (!blocks.length) return <div className="st-notes st-notes--empty">{empty}</div>;
  return (
    <div className="st-notes">
      {blocks.map((b, i) => {
        const k = `b${i}`;
        switch (b.k) {
          case 'h':
            return b.level <= 2 ? <h4 key={k}>{inline(b.text, k)}</h4> : <h5 key={k}>{inline(b.text, k)}</h5>;
          case 'p':
            return <p key={k}>{inline(b.text, k)}</p>;
          case 'quote':
            return <blockquote key={k}>{inline(b.text, k)}</blockquote>;
          case 'code':
            return <pre key={k}><code>{b.text}</code></pre>;
          case 'ul':
            return <ul key={k}>{b.items.map((it, j) => <li key={j}>{inline(it, `${k}-${j}`)}</li>)}</ul>;
          case 'ol':
            return <ol key={k}>{b.items.map((it, j) => <li key={j}>{inline(it, `${k}-${j}`)}</li>)}</ol>;
        }
        return null;
      })}
    </div>
  );
}
