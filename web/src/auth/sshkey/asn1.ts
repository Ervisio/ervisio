// Minimal DER reader for PKCS#1 / SEC1 / PKCS#8 private keys.
import { SshKeyError } from './errors.ts';
import type { Bytes } from './bytes.ts';

export interface Node {
  tag: number;
  /** Content bytes. */
  value: Bytes;
  /** Children, for constructed tags (SEQUENCE, context [n]). */
  children?: Node[];
}

const bad = () => new SshKeyError('corrupt_key', 'The key data is not valid DER.');

function readLen(b: Bytes, off: number): [number, number] {
  if (off >= b.length) throw bad();
  const first = b[off++];
  if (first < 0x80) return [first, off];
  const n = first & 0x7f;
  if (n === 0 || n > 4 || off + n > b.length) throw bad();
  let len = 0;
  for (let i = 0; i < n; i++) len = len * 256 + b[off++];
  return [len, off];
}

/** Parses one DER element (and, recursively, constructed ones) starting at b[0]; trailing bytes are an error unless allowTrailing. */
export function parse(b: Bytes, allowTrailing = false): Node {
  const [node, end] = parseAt(b, 0, 0);
  if (!allowTrailing && end !== b.length) throw bad();
  return node;
}

function parseAt(b: Bytes, off: number, depth: number): [Node, number] {
  if (depth > 16 || off + 2 > b.length) throw bad();
  const tag = b[off++];
  if ((tag & 0x1f) === 0x1f) throw bad(); // high tag numbers are not used here
  const [len, start] = readLen(b, off);
  const end = start + len;
  if (end > b.length) throw bad();
  const value = b.subarray(start, end);
  const node: Node = { tag, value };
  if (tag & 0x20) {
    node.children = [];
    let o = start;
    while (o < end) {
      const [child, next] = parseAt(b, o, depth + 1);
      node.children.push(child);
      o = next;
    }
  }
  return [node, end];
}

export const SEQUENCE = 0x30;
export const INTEGER = 0x02;
export const OCTET_STRING = 0x04;
export const BIT_STRING = 0x03;
export const OID = 0x06;
export const NULL = 0x05;

export function expect(n: Node | undefined, tag: number): Node {
  if (!n || n.tag !== tag) throw bad();
  return n;
}

export function seq(n: Node | undefined): Node[] {
  return expect(n, SEQUENCE).children!;
}

/** INTEGER as unsigned big-endian bytes without leading zeros. */
export function uint(n: Node | undefined): Bytes {
  const v = expect(n, INTEGER).value;
  if (v.length && v[0] & 0x80) throw bad(); // negative
  let i = 0;
  while (i < v.length - 1 && v[i] === 0) i++;
  return v.slice(i);
}

export function smallInt(n: Node | undefined): number {
  const v = uint(n);
  if (v.length > 4) throw bad();
  let x = 0;
  for (const b of v) x = x * 256 + b;
  return x;
}

export function oid(n: Node | undefined): string {
  const v = expect(n, OID).value;
  if (!v.length) throw bad();
  const parts = [Math.floor(v[0] / 40), v[0] % 40];
  let acc = 0;
  for (let i = 1; i < v.length; i++) {
    acc = acc * 128 + (v[i] & 0x7f);
    if (!(v[i] & 0x80)) {
      parts.push(acc);
      acc = 0;
    }
  }
  return parts.join('.');
}

/** BIT STRING content without the unused-bits byte (must be 0). */
export function bits(n: Node | undefined): Bytes {
  const v = expect(n, BIT_STRING).value;
  if (!v.length || v[0] !== 0) throw bad();
  return v.subarray(1);
}

/** Context-specific constructed [k] child of a SEQUENCE, if present. */
export function context(children: Node[], k: number): Node | undefined {
  return children.find((c) => c.tag === (0xa0 | k));
}
