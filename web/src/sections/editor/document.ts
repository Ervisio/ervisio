export type EditorMode = 'text' | 'code';
export type LineEnding = 'LF' | 'CRLF';
export interface TextDocument {
  id: string;
  path: string;
  text: string;
  saved: string;
  encoding: string;
  mtime: number;
  eol: LineEnding;
  admin: boolean;
  readOnly: boolean;
}

export const normalizeText = (text: string) => text.replace(/\r\n/g, '\n');
export const serializeText = (text: string, eol: LineEnding) =>
  eol === 'CRLF' ? normalizeText(text).replace(/\n/g, '\r\n') : normalizeText(text);
export const documentName = (path: string) => path.split(/[\\/]/).pop() || path;
export const preferredEditor = (name: string): EditorMode =>
  /\.(txt|log)$/i.test(name) ? 'text' : 'code';
export const isDocumentDirty = (doc: TextDocument) => doc.text !== doc.saved;
export function newDocument(): TextDocument {
  return {
    id: crypto.randomUUID(),
    path: '',
    text: '',
    saved: '',
    encoding: 'utf-8',
    mtime: 0,
    eol: 'LF',
    admin: false,
    readOnly: false,
  };
}
export function textPosition(text: string, offset: number) {
  const before = text.slice(0, Math.max(0, Math.min(offset, text.length)));
  const lines = before.split('\n');
  return { line: lines.length, column: lines[lines.length - 1].length + 1 };
}
