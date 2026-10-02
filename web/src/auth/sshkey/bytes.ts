// Byte helpers for the SSH key code: base64, SSH wire strings, mpints.
import { SshKeyError } from './errors.ts';

export type Bytes = Uint8Array<ArrayBuffer>;

export const te = new TextEncoder();

export function utf8(s: string): Bytes {
  return te.encode(s) as Bytes;
}

export function concat(...parts: Uint8Array[]): Bytes {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}

export function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i] ^ b[i];
  return d === 0;
}

export function wipe(...bufs: (Uint8Array | null | undefined)[]) {
  for (const b of bufs) b?.fill(0);
}

export function b64decode(s: string): Bytes {
  const clean = s.replace(/[\s]/g, '').replace(/-/g, '+').replace(/_/g, '/');
  let bin: string;
  try {
    bin = atob(clean);
  } catch {
    throw new SshKeyError('corrupt_key', 'The key text is not valid base64.');
  }
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function b64encode(b: Uint8Array): string {
  let s = '';
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000));
  return btoa(s);
}

export function b64url(b: Uint8Array): string {
  return b64encode(b).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function hex(b: Uint8Array): string {
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

export function fromHex(h: string): Bytes {
  const out = new Uint8Array(h.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(h.slice(2 * i, 2 * i + 2), 16);
  return out;
}

export function u32(n: number): Bytes {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, n >>> 0);
  return b;
}

/** SSH wire "string": uint32 length + bytes. */
export function sshString(b: Uint8Array | string): Bytes {
  const v = typeof b === 'string' ? utf8(b) : b;
  return concat(u32(v.length), v);
}

/** Unsigned big-endian bytes → SSH mpint (minimal, with a 0 byte when the top bit is set). */
export function mpint(b: Uint8Array): Bytes {
  let i = 0;
  while (i < b.length && b[i] === 0) i++;
  let v = b.subarray(i);
  if (v.length && v[0] & 0x80) v = concat(new Uint8Array([0]), v);
  return sshString(v);
}

export function bytesToBigInt(b: Uint8Array): bigint {
  let n = 0n;
  for (const x of b) n = (n << 8n) | BigInt(x);
  return n;
}

export function bigIntToBytes(n: bigint, len?: number): Bytes {
  let h = n.toString(16);
  if (h.length % 2) h = '0' + h;
  let b = fromHex(h);
  if (len !== undefined) {
    if (b.length > len) throw new SshKeyError('corrupt_key', 'Number too large.');
    if (b.length < len) b = concat(new Uint8Array(len - b.length), b);
  }
  return b;
}

/** Strips leading zero bytes (mpint → unsigned magnitude). */
export function stripZeros(b: Uint8Array): Bytes {
  let i = 0;
  while (i < b.length - 1 && b[i] === 0) i++;
  return b.slice(i);
}

/** Reads SSH wire data. Every read throws corrupt_key when the data is short. */
export class Reader {
  buf: Bytes;
  off = 0;
  constructor(buf: Bytes) {
    this.buf = buf;
  }
  get left(): number {
    return this.buf.length - this.off;
  }
  bytes(n: number): Bytes {
    if (n < 0 || this.off + n > this.buf.length) throw new SshKeyError('corrupt_key', 'The key data is truncated.');
    const v = this.buf.subarray(this.off, this.off + n);
    this.off += n;
    return v;
  }
  u32(): number {
    const b = this.bytes(4);
    return new DataView(b.buffer, b.byteOffset, 4).getUint32(0);
  }
  string(): Bytes {
    return this.bytes(this.u32());
  }
  text(): string {
    return new TextDecoder().decode(this.string());
  }
  rest(): Bytes {
    return this.bytes(this.left);
  }
}
