// bcrypt_pbkdf as in OpenBSD (lib/libutil/bcrypt_pbkdf.c), the key derivation of encrypted OpenSSH
// private keys. Blowfish follows OpenBSD's blf.c (Blowfish_initstate / expandstate / expand0state).
// SHA-512 comes from WebCrypto (or an injected function in tests).
import { P_INIT, S_INIT } from './blowfish-init.ts';
import type { Bytes } from './bytes.ts';

class Blowfish {
  P = new Uint32Array(18);
  S = new Uint32Array(1024);
  constructor() {
    this.P.set(P_INIT);
    this.S.set(S_INIT);
  }

  private F(x: number): number {
    const S = this.S;
    return (((S[x >>> 24] + S[256 + ((x >>> 16) & 0xff)]) ^ S[512 + ((x >>> 8) & 0xff)]) + S[768 + (x & 0xff)]) >>> 0;
  }

  /** Encrypts the block lr[o], lr[o+1] in place. */
  encipher(lr: Uint32Array, o: number) {
    const P = this.P;
    let l = lr[o] ^ P[0];
    let r = lr[o + 1];
    for (let i = 1; i <= 16; i += 2) {
      r ^= this.F(l) ^ P[i];
      l ^= this.F(r) ^ P[i + 1];
    }
    lr[o] = (r ^ P[17]) >>> 0;
    lr[o + 1] = l >>> 0;
  }

  /** Blowfish_expandstate (data != null) or Blowfish_expand0state (data == null). */
  expand(key: Uint8Array, data: Uint8Array | null) {
    const P = this.P;
    const S = this.S;
    const kj = { j: 0 };
    for (let i = 0; i < 18; i++) P[i] = (P[i] ^ streamToWord(key, kj)) >>> 0;
    const dj = { j: 0 };
    const lr = new Uint32Array(2);
    for (let i = 0; i < 18; i += 2) {
      if (data) {
        lr[0] ^= streamToWord(data, dj);
        lr[1] ^= streamToWord(data, dj);
      }
      this.encipher(lr, 0);
      P[i] = lr[0];
      P[i + 1] = lr[1];
    }
    for (let i = 0; i < 1024; i += 2) {
      if (data) {
        lr[0] ^= streamToWord(data, dj);
        lr[1] ^= streamToWord(data, dj);
      }
      this.encipher(lr, 0);
      S[i] = lr[0];
      S[i + 1] = lr[1];
    }
  }
}

/** Blowfish_stream2word: the next 4 bytes of data (cyclic), big-endian. */
function streamToWord(data: Uint8Array, pos: { j: number }): number {
  let w = 0;
  let j = pos.j;
  for (let i = 0; i < 4; i++) {
    w = ((w << 8) | data[j]) >>> 0;
    j = (j + 1) % data.length;
  }
  pos.j = j;
  return w;
}

const MAGIC = new TextEncoder().encode('OxychromaticBlowfishSwatDynamite');

/** bcrypt_hash: 32-byte output from 64-byte SHA-512 digests of the password and salt. */
export function bcryptHash(sha2pass: Uint8Array, sha2salt: Uint8Array): Bytes {
  const bf = new Blowfish();
  bf.expand(sha2pass, sha2salt);
  for (let i = 0; i < 64; i++) {
    bf.expand(sha2salt, null);
    bf.expand(sha2pass, null);
  }
  const cdata = new Uint32Array(8);
  const pos = { j: 0 };
  for (let i = 0; i < 8; i++) cdata[i] = streamToWord(MAGIC, pos);
  for (let i = 0; i < 64; i++) for (let k = 0; k < 8; k += 2) bf.encipher(cdata, k);
  const out = new Uint8Array(32);
  for (let i = 0; i < 8; i++) {
    out[4 * i + 3] = cdata[i] >>> 24;
    out[4 * i + 2] = (cdata[i] >>> 16) & 0xff;
    out[4 * i + 1] = (cdata[i] >>> 8) & 0xff;
    out[4 * i] = cdata[i] & 0xff;
  }
  bf.P.fill(0);
  bf.S.fill(0);
  return out;
}

export type Sha512 = (data: Uint8Array) => Promise<Uint8Array>;

export const webSha512: Sha512 = async (data) => new Uint8Array(await crypto.subtle.digest('SHA-512', data as Bytes));

/**
 * bcrypt_pbkdf(pass, salt, rounds) → keylen bytes. Async only because SHA-512 comes from WebCrypto;
 * between rounds it awaits WebCrypto, which lets the page breathe (one round of Blowfish work is ~30 ms).
 */
export async function bcryptPbkdf(pass: Uint8Array, salt: Uint8Array, rounds: number, keylen: number, sha512: Sha512 = webSha512): Promise<Bytes> {
  if (rounds < 1 || rounds > 1 << 16 || keylen < 1 || keylen > 1024 || salt.length === 0 || salt.length > 1 << 20) {
    throw new RangeError('bcrypt_pbkdf: bad parameters');
  }
  const stride = Math.ceil(keylen / 32);
  let amt = Math.ceil(keylen / stride);
  const key = new Uint8Array(keylen);
  const sha2pass = await sha512(pass);
  const countsalt = new Uint8Array(salt.length + 4);
  countsalt.set(salt);
  let left = keylen;
  for (let count = 1; left > 0; count++) {
    countsalt[salt.length] = (count >>> 24) & 0xff;
    countsalt[salt.length + 1] = (count >>> 16) & 0xff;
    countsalt[salt.length + 2] = (count >>> 8) & 0xff;
    countsalt[salt.length + 3] = count & 0xff;
    let sha2salt = await sha512(countsalt);
    let tmp = bcryptHash(sha2pass, sha2salt);
    const out = tmp.slice();
    for (let r = 1; r < rounds; r++) {
      sha2salt = await sha512(tmp);
      tmp = bcryptHash(sha2pass, sha2salt);
      for (let j = 0; j < out.length; j++) out[j] ^= tmp[j];
    }
    amt = Math.min(amt, left);
    let i = 0;
    for (; i < amt; i++) {
      const dest = i * stride + (count - 1);
      if (dest >= keylen) break;
      key[dest] = out[i];
    }
    left -= i;
    out.fill(0);
    tmp.fill(0);
  }
  sha2pass.fill(0);
  return key;
}
