/** Small syntax highlighters for the stack editor: YAML (compose files) and dotenv. Output is escaped HTML per line. */

const esc = (s: string): string => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const span = (cls: string, s: string): string => (s ? `<span class="dk-t-${cls}">${esc(s)}</span>` : '');

/** Value part of a YAML line: strings, variables, numbers, flow lists, trailing comments. */
function value(src: string): string {
  let out = '';
  let i = 0;
  while (i < src.length) {
    const rest = src.slice(i);
    let m: RegExpExecArray | null;
    if ((m = /^\s+#.*$/.exec(rest))) {
      out += esc(m[0].match(/^\s*/)![0]) + span('c', m[0].trimStart());
      i = src.length;
    } else if ((m = /^"(?:[^"\\]|\\.)*"?|^'(?:[^']|'')*'?/.exec(rest))) {
      out += quoted(m[0]);
      i += m[0].length;
    } else if ((m = /^\$\{[^}]*\}?|^\$[A-Za-z_][A-Za-z0-9_]*/.exec(rest))) {
      out += span('v', m[0]);
      i += m[0].length;
    } else if ((m = /^(?:true|false|null|yes|no|~)(?=$|[\s,\]}])|^-?\d+(?:\.\d+)?(?=$|[\s,\]}])/.exec(rest))) {
      out += span('n', m[0]);
      i += m[0].length;
    } else if ((m = /^[[\]{},]/.exec(rest))) {
      out += esc(m[0]);
      i++;
    } else {
      m = /^[^\s,[\]{}$"']+|^\s+|^./.exec(rest)!;
      out += /^\s+$/.test(m[0]) ? esc(m[0]) : span('s', m[0]);
      i += m[0].length;
    }
  }
  return out;
}

/** A quoted string, with ${VARS} inside shown in the variable colour. */
function quoted(s: string): string {
  return s
    .split(/(\$\{[^}]*\}?|\$[A-Za-z_][A-Za-z0-9_]*)/)
    .map((p, i) => (i % 2 ? span('v', p) : span('s', p)))
    .join('');
}

export function highlightYamlLine(line: string): string {
  if (/^\s*#/.test(line)) return span('c', line);
  if (/^(---|\.\.\.)\s*$/.test(line)) return span('c', line);
  const m = /^(\s*(?:-\s+)*)((?:"[^"]*"|'[^']*'|[^\s:#'"\-[{][^:#]*?|-[^\s:#][^:#]*?)(?=:(?:\s|$)))(:)(.*)$/.exec(line);
  if (m) return esc(m[1]) + span('k', m[2]) + esc(m[3]) + value(m[4]);
  const d = /^(\s*(?:-\s+)+)(.*)$/.exec(line);
  if (d) return esc(d[1]) + value(d[2]);
  return value(line);
}

export function highlightEnvLine(line: string): string {
  if (/^\s*#/.test(line)) return span('c', line);
  const m = /^(\s*(?:export\s+)?)([A-Za-z_][A-Za-z0-9_.-]*)(=)(.*)$/.exec(line);
  if (m) return esc(m[1]) + span('k', m[2]) + esc(m[3]) + (/^["']/.test(m[4]) ? quoted(m[4]) : /\$/.test(m[4]) ? quoted(m[4]) : span('s', m[4]));
  return esc(line);
}

export type Lang = 'yaml' | 'env' | 'plain';

export function highlightLine(line: string, lang: Lang): string {
  return lang === 'yaml' ? highlightYamlLine(line) : lang === 'env' ? highlightEnvLine(line) : esc(line);
}
