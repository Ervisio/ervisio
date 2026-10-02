// Private key parsing and signing, entirely in the browser.
//
// Formats: OpenSSH ("-----BEGIN OPENSSH PRIVATE KEY-----", plain or encrypted with bcrypt_pbkdf +
// aes{128,192,256}-{ctr,cbc} / aes{128,256}-gcm@openssh.com), PKCS#8 ("PRIVATE KEY", and
// "ENCRYPTED PRIVATE KEY" with PBES2/PBKDF2 + AES-CBC), PKCS#1 "RSA PRIVATE KEY" and SEC1
// "EC PRIVATE KEY" (plain or with OpenSSL's legacy "DEK-Info: AES-*-CBC" encryption).
// Key types: ssh-ed25519, ecdsa-sha2-nistp256/384/521, ssh-rsa (≥ 2048 bits, signed as rsa-sha2-512).
import * as ed from '@noble/ed25519';
import * as asn1 from './asn1.ts';
import { bcryptPbkdf } from './bcrypt.ts';
import {
  b64decode, b64encode, b64url, bigIntToBytes, bytesToBigInt, concat, equal, fromHex, mpint, Reader, sshString, stripZeros, utf8, wipe,
  type Bytes,
} from './bytes.ts';
import { SshKeyError } from './errors.ts';
import { md5 } from './md5.ts';

export type KeyType = 'ssh-ed25519' | 'ecdsa-sha2-nistp256' | 'ecdsa-sha2-nistp384' | 'ecdsa-sha2-nistp521' | 'ssh-rsa';

export interface PrivateKey {
  type: KeyType;
  /** Key size in bits (256 for ed25519). */
  bits: number;
  /** Comment stored in the key (OpenSSH format only), else "". */
  comment: string;
  /** Public key in authorized_keys form: "type base64 [comment]". */
  publicKey: string;
  /** OpenSSH-style fingerprint, "SHA256:…" (no padding). */
  fingerprint: string;
  /** Signs data; returns an SSH signature blob (string format, string signature). */
  sign(data: Uint8Array): Promise<Bytes>;
}

/** Whether the text looks encrypted (so the UI can ask for a passphrase before trying). Never throws. */
export function needsPassphrase(text: string): boolean {
  try {
    const block = findBlock(text);
    if (!block) return false;
    if (block.label === 'ENCRYPTED PRIVATE KEY') return true;
    if (block.headers['proc-type']?.includes('ENCRYPTED')) return true;
    if (block.label === 'OPENSSH PRIVATE KEY') {
      const r = new Reader(b64decode(block.body));
      r.bytes(15);
      return r.text() !== 'none';
    }
  } catch {
    /* not a key */
  }
  return false;
}

// ---------------------------------------------------------------- text → block

interface Block {
  label: string;
  headers: Record<string, string>;
  body: string;
}

function findBlock(text: string): Block | null {
  const re = /-----BEGIN ([A-Z0-9 ]+)-----([\s\S]*?)-----END \1-----/g;
  let m: RegExpExecArray | null;
  let first: Block | null = null;
  while ((m = re.exec(text))) {
    const label = m[1];
    const lines = m[2].replace(/\r/g, '').split('\n');
    const headers: Record<string, string> = {};
    const body: string[] = [];
    for (const raw of lines) {
      const l = raw.trim();
      if (!l) continue;
      const h = /^([A-Za-z-]+):\s*(.*)$/.exec(l);
      if (h && !body.length) headers[h[1].toLowerCase()] = h[2];
      else body.push(l);
    }
    const b = { label, headers, body: body.join('') };
    if (label.includes('PRIVATE KEY')) return b;
    first ??= b;
  }
  return first;
}

const SUPPORTED_TYPES: KeyType[] = ['ssh-ed25519', 'ecdsa-sha2-nistp256', 'ecdsa-sha2-nistp384', 'ecdsa-sha2-nistp521', 'ssh-rsa'];

/**
 * Parses a private key. `passphrase` is needed for encrypted keys (passphrase_required otherwise).
 * Throws SshKeyError (see errors.ts).
 */
export async function parsePrivateKey(text: string, passphrase = ''): Promise<PrivateKey> {
  if (!globalThis.crypto?.subtle) throw new SshKeyError('crypto_unavailable', 'This browser does not offer WebCrypto here (the page must be served over https or from localhost).');
  const t = (text ?? '').trim();
  if (!t) throw new SshKeyError('not_a_key', 'No key given.');
  if (/^PuTTY-User-Key-File-/m.test(t)) throw new SshKeyError('unsupported_format', 'PuTTY .ppk keys are not supported: export the key in OpenSSH format (PuTTYgen › Conversions).');
  if (/^---- BEGIN SSH2 ENCRYPTED PRIVATE KEY ----/m.test(t)) throw new SshKeyError('unsupported_format', 'SSH2 (RFC 4716) private keys are not supported: convert the key to OpenSSH format.');
  if (/^(ssh-|ecdsa-|sk-)[a-z0-9@.-]+ AAAA/m.test(t) || /BEGIN SSH2 PUBLIC KEY|BEGIN (RSA )?PUBLIC KEY/.test(t)) {
    throw new SshKeyError('public_key_given', 'This is a public key. Choose the private key (the file without .pub).');
  }
  const block = findBlock(t);
  if (!block) throw new SshKeyError('not_a_key', 'This is not a private key.');
  switch (block.label) {
    case 'OPENSSH PRIVATE KEY':
      return parseOpenSSH(b64decode(block.body), passphrase);
    case 'PRIVATE KEY':
      return fromPkcs8(b64decode(block.body));
    case 'ENCRYPTED PRIVATE KEY':
      return fromPkcs8(await decryptPkcs8(b64decode(block.body), passphrase));
    case 'RSA PRIVATE KEY':
      return fromPkcs1(await legacyBody(block, passphrase));
    case 'EC PRIVATE KEY':
      return fromSec1(await legacyBody(block, passphrase), null);
    case 'DSA PRIVATE KEY':
      throw new SshKeyError('unsupported_key_type', 'DSA keys are not supported.');
    default:
      throw new SshKeyError('unsupported_format', `"${block.label}" keys are not supported.`);
  }
}

// ---------------------------------------------------------------- OpenSSH format

const AUTH_MAGIC = 'openssh-key-v1\0';

interface CipherSpec {
  keyLen: number;
  ivLen: number;
  mode: 'ctr' | 'cbc' | 'gcm';
  authLen: number;
}

const CIPHERS: Record<string, CipherSpec> = {
  'aes128-ctr': { keyLen: 16, ivLen: 16, mode: 'ctr', authLen: 0 },
  'aes192-ctr': { keyLen: 24, ivLen: 16, mode: 'ctr', authLen: 0 },
  'aes256-ctr': { keyLen: 32, ivLen: 16, mode: 'ctr', authLen: 0 },
  'aes128-cbc': { keyLen: 16, ivLen: 16, mode: 'cbc', authLen: 0 },
  'aes192-cbc': { keyLen: 24, ivLen: 16, mode: 'cbc', authLen: 0 },
  'aes256-cbc': { keyLen: 32, ivLen: 16, mode: 'cbc', authLen: 0 },
  'aes128-gcm@openssh.com': { keyLen: 16, ivLen: 12, mode: 'gcm', authLen: 16 },
  'aes256-gcm@openssh.com': { keyLen: 32, ivLen: 12, mode: 'gcm', authLen: 16 },
};

/**
 * Type, size and fingerprint of an OpenSSH private key without its passphrase: that format keeps the public
 * key in clear. Null for other formats or anything unreadable.
 */
export async function publicInfo(text: string): Promise<{ type: KeyType; bits: number; fingerprint: string } | null> {
  try {
    const block = findBlock((text ?? '').trim());
    if (!block || block.label !== 'OPENSSH PRIVATE KEY' || !globalThis.crypto?.subtle) return null;
    const r = new Reader(b64decode(block.body));
    if (new TextDecoder().decode(r.bytes(AUTH_MAGIC.length)) !== AUTH_MAGIC) return null;
    r.text();
    r.text();
    r.string();
    if (r.u32() !== 1) return null;
    const blob = r.string();
    const pr = new Reader(blob);
    const type = pr.text();
    checkType(type);
    let bits = 256;
    if (type === 'ssh-rsa') {
      pr.string(); // e
      const n = pr.string();
      let i = 0;
      while (i < n.length && n[i] === 0) i++;
      bits = (n.length - i) * 8 - (i < n.length ? Math.clz32(n[i]) - 24 : 0);
    } else if (type !== 'ssh-ed25519') {
      bits = Number(type.slice(-3));
    }
    const fp = new Uint8Array(await crypto.subtle.digest('SHA-256', blob));
    return { type, bits, fingerprint: 'SHA256:' + b64encode(fp).replace(/=+$/, '') };
  } catch {
    return null;
  }
}

async function parseOpenSSH(raw: Bytes, passphrase: string): Promise<PrivateKey> {
  const r = new Reader(raw);
  if (new TextDecoder().decode(r.bytes(AUTH_MAGIC.length)) !== AUTH_MAGIC) throw new SshKeyError('corrupt_key', 'Not an OpenSSH private key.');
  const cipher = r.text();
  const kdf = r.text();
  const kdfOpts = r.string();
  const n = r.u32();
  if (n !== 1) throw new SshKeyError('unsupported_format', 'Files holding several keys are not supported.');
  const pubBlob = r.string();
  const pubType = new Reader(pubBlob).text();
  checkType(pubType);

  let plain: Bytes;
  if (cipher === 'none') {
    if (kdf !== 'none') throw new SshKeyError('corrupt_key', 'Inconsistent key encryption fields.');
    plain = r.string();
  } else {
    const spec = CIPHERS[cipher];
    if (!spec) throw new SshKeyError('unsupported_cipher', `Keys encrypted with ${cipher} are not supported. Re-encrypt it: ssh-keygen -p -Z aes256-ctr -f <key>.`);
    if (kdf !== 'bcrypt') throw new SshKeyError('unsupported_cipher', `Key derivation ${kdf} is not supported.`);
    if (!passphrase) throw new SshKeyError('passphrase_required', 'This key is protected by a passphrase.');
    const ko = new Reader(kdfOpts);
    const salt = ko.string();
    const rounds = ko.u32();
    if (rounds < 1 || rounds > 1 << 16) throw new SshKeyError('corrupt_key', 'Unreasonable bcrypt rounds.');
    const len = r.u32();
    const ct = r.bytes(len);
    const tag = r.bytes(spec.authLen);
    if (len % 16 !== 0) throw new SshKeyError('corrupt_key', 'Encrypted section has a bad length.');
    const pass = utf8(passphrase);
    const kiv = await bcryptPbkdf(pass, salt, rounds, spec.keyLen + spec.ivLen);
    wipe(pass);
    try {
      plain = await aesDecrypt(spec.mode, kiv.slice(0, spec.keyLen), kiv.slice(spec.keyLen), concat(ct, tag));
    } catch (e) {
      if (e instanceof SshKeyError) throw e;
      // GCM: authentication failed = wrong passphrase.
      throw new SshKeyError('bad_passphrase', 'Wrong passphrase.');
    } finally {
      wipe(kiv);
    }
  }
  const p = new Reader(plain);
  const c1 = p.u32();
  const c2 = p.u32();
  if (c1 !== c2) {
    wipe(plain);
    throw new SshKeyError(cipher === 'none' ? 'corrupt_key' : 'bad_passphrase', cipher === 'none' ? 'The key data is damaged.' : 'Wrong passphrase.');
  }
  const type = p.text();
  if (type !== pubType) throw new SshKeyError('corrupt_key', 'Public and private key types differ.');
  let key: Material;
  if (type === 'ssh-ed25519') {
    const pub = p.string();
    const priv = p.string();
    if (pub.length !== 32 || priv.length !== 64 || !equal(priv.subarray(32), pub)) throw new SshKeyError('corrupt_key', 'Damaged ed25519 key.');
    key = { kind: 'ed25519', seed: priv.slice(0, 32), pub: pub.slice() };
  } else if (type === 'ssh-rsa') {
    const nn = stripZeros(p.string());
    const e = stripZeros(p.string());
    const d = stripZeros(p.string());
    const qi = stripZeros(p.string());
    const pp = stripZeros(p.string());
    const q = stripZeros(p.string());
    key = rsaMaterial(nn, e, d, pp, q, qi);
  } else {
    const curveName = p.text();
    const Q = p.string().slice();
    const d = stripZeros(p.string());
    key = ecMaterial(type as KeyType, curveName, Q, d);
  }
  const comment = p.text();
  // Padding 1, 2, 3, … up to the block size.
  const pad = p.rest();
  for (let i = 0; i < pad.length; i++) if (pad[i] !== i + 1) throw new SshKeyError(cipher === 'none' ? 'corrupt_key' : 'bad_passphrase', 'The key data is damaged.');
  wipe(plain);
  if (!equal(publicBlob(key), pubBlob)) throw new SshKeyError('corrupt_key', 'The public key does not match the private key.');
  return build(key, comment);
}

function checkType(t: string): asserts t is KeyType {
  if (!(SUPPORTED_TYPES as string[]).includes(t)) {
    const why = t.startsWith('sk-') ? 'Security-key (FIDO) keys need the hardware key and cannot be used in a browser.' : t === 'ssh-dss' ? 'DSA keys are not supported.' : `Key type ${t} is not supported.`;
    throw new SshKeyError('unsupported_key_type', why);
  }
}

// ---------------------------------------------------------------- AES via WebCrypto

async function aesDecrypt(mode: 'ctr' | 'cbc' | 'gcm', key: Bytes, iv: Bytes, data: Bytes): Promise<Bytes> {
  const s = crypto.subtle;
  if (mode === 'ctr') {
    const k = await s.importKey('raw', key, 'AES-CTR', false, ['decrypt']);
    return new Uint8Array(await s.decrypt({ name: 'AES-CTR', counter: iv, length: 128 }, k, data));
  }
  if (mode === 'gcm') {
    const k = await s.importKey('raw', key, 'AES-GCM', false, ['decrypt']);
    return new Uint8Array(await s.decrypt({ name: 'AES-GCM', iv, tagLength: 128 }, k, data));
  }
  return cbcNoPadding(key, iv, data);
}

/**
 * AES-CBC decryption without padding (OpenSSH pads itself). WebCrypto always checks PKCS#7 padding,
 * so a final block that decrypts to a full padding block is appended: E_k(0x10…10 XOR last block),
 * which is the first block of encrypting sixteen 0x10 bytes with the last ciphertext block as IV.
 */
async function cbcNoPadding(key: Bytes, iv: Bytes, data: Bytes): Promise<Bytes> {
  if (data.length === 0 || data.length % 16) throw new SshKeyError('corrupt_key', 'Encrypted section has a bad length.');
  const k = await crypto.subtle.importKey('raw', key, 'AES-CBC', false, ['encrypt', 'decrypt']);
  const last = data.slice(data.length - 16);
  const extra = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-CBC', iv: last }, k, new Uint8Array(16).fill(16))).slice(0, 16);
  return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-CBC', iv }, k, concat(data, extra)));
}

// ---------------------------------------------------------------- PEM / PKCS#8

const OID_RSA = '1.2.840.113549.1.1.1';
const OID_EC = '1.2.840.10045.2.1';
const OID_ED25519 = '1.3.101.112';
const CURVES: Record<string, { type: KeyType; ssh: string; jwk: string; size: number; hash: string; bits: number }> = {
  '1.2.840.10045.3.1.7': { type: 'ecdsa-sha2-nistp256', ssh: 'nistp256', jwk: 'P-256', size: 32, hash: 'SHA-256', bits: 256 },
  '1.3.132.0.34': { type: 'ecdsa-sha2-nistp384', ssh: 'nistp384', jwk: 'P-384', size: 48, hash: 'SHA-384', bits: 384 },
  '1.3.132.0.35': { type: 'ecdsa-sha2-nistp521', ssh: 'nistp521', jwk: 'P-521', size: 66, hash: 'SHA-512', bits: 521 },
};
const curveBySsh = (name: string) => Object.values(CURVES).find((c) => c.ssh === name);

async function fromPkcs8(der: Bytes): Promise<PrivateKey> {
  const top = asn1.seq(asn1.parse(der));
  const alg = asn1.seq(top[1]);
  const id = asn1.oid(alg[0]);
  const inner = asn1.expect(top[2], asn1.OCTET_STRING).value;
  if (id === OID_RSA) return fromPkcs1(inner);
  if (id === OID_EC) {
    const curve = CURVES[asn1.oid(alg[1])];
    if (!curve) throw new SshKeyError('unsupported_key_type', 'This elliptic curve is not supported (P-256, P-384 and P-521 are).');
    return fromSec1(inner, curve);
  }
  if (id === OID_ED25519) {
    const seed = asn1.expect(asn1.parse(inner), asn1.OCTET_STRING).value.slice();
    if (seed.length !== 32) throw new SshKeyError('corrupt_key', 'Damaged ed25519 key.');
    const pub = await ed.getPublicKeyAsync(seed);
    return build({ kind: 'ed25519', seed, pub: new Uint8Array(pub) }, '');
  }
  throw new SshKeyError('unsupported_key_type', 'This key algorithm is not supported.');
}

async function fromPkcs1(der: Bytes): Promise<PrivateKey> {
  const s = asn1.seq(asn1.parse(der));
  if (asn1.smallInt(s[0]) !== 0) throw new SshKeyError('unsupported_format', 'Multi-prime RSA keys are not supported.');
  const [n, e, d, p, q] = [1, 2, 3, 4, 5].map((i) => asn1.uint(s[i]));
  const qi = asn1.uint(s[8]);
  return build(rsaMaterial(n, e, d, p, q, qi), '');
}

async function fromSec1(der: Bytes, curveHint: (typeof CURVES)[string] | null): Promise<PrivateKey> {
  const s = asn1.seq(asn1.parse(der));
  if (asn1.smallInt(s[0]) !== 1) throw new SshKeyError('corrupt_key', 'Unknown EC key version.');
  const d = stripZeros(asn1.expect(s[1], asn1.OCTET_STRING).value);
  const params = asn1.context(s, 0);
  const curve = params ? CURVES[asn1.oid(params.children![0])] : curveHint;
  if (!curve) throw new SshKeyError('unsupported_key_type', 'This elliptic curve is not supported (P-256, P-384 and P-521 are).');
  const pubNode = asn1.context(s, 1);
  let Q: Bytes;
  if (pubNode) {
    Q = asn1.bits(pubNode.children![0]).slice();
  } else {
    // No public point stored: let WebCrypto derive it.
    const jwk = await deriveEcPublic(curve.jwk, d, curve.size);
    Q = concat(new Uint8Array([4]), b64decode(jwk.x!), b64decode(jwk.y!));
  }
  return build(ecMaterial(curve.type, curve.ssh, Q, d), '');
}

async function deriveEcPublic(crv: string, d: Bytes, size: number): Promise<JsonWebKey> {
  // PKCS#8 wrapping of a SEC1 key without the public point; WebCrypto computes it on import.
  const curveOid = Object.entries(CURVES).find(([, c]) => c.jwk === crv)![0];
  const sec1 = derSeq(derInt(new Uint8Array([1])), derTag(0x04, bigIntToBytes(bytesToBigInt(d), size)));
  const pkcs8 = derSeq(derInt(new Uint8Array([0])), derSeq(derOid(OID_EC), derOid(curveOid)), derTag(0x04, sec1));
  const k = await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'ECDSA', namedCurve: crv }, true, ['sign']);
  return crypto.subtle.exportKey('jwk', k);
}

function derLen(n: number): Bytes {
  if (n < 0x80) return new Uint8Array([n]);
  const b = bigIntToBytes(BigInt(n));
  return concat(new Uint8Array([0x80 | b.length]), b);
}
function derTag(tag: number, v: Uint8Array): Bytes {
  return concat(new Uint8Array([tag]), derLen(v.length), v);
}
function derSeq(...parts: Uint8Array[]): Bytes {
  return derTag(0x30, concat(...parts));
}
function derInt(v: Uint8Array): Bytes {
  return derTag(0x02, v[0] & 0x80 ? concat(new Uint8Array([0]), v) : v);
}
function derOid(s: string): Bytes {
  const p = s.split('.').map(Number);
  const out = [40 * p[0] + p[1]];
  for (const x of p.slice(2)) {
    const enc = [x & 0x7f];
    for (let v = Math.floor(x / 128); v > 0; v = Math.floor(v / 128)) enc.unshift((v & 0x7f) | 0x80);
    out.push(...enc);
  }
  return derTag(0x06, new Uint8Array(out));
}

/** OpenSSL legacy PEM body ("Proc-Type: 4,ENCRYPTED" + "DEK-Info: AES-128-CBC,<iv>"). */
async function legacyBody(block: Block, passphrase: string): Promise<Bytes> {
  const der = b64decode(block.body);
  if (!block.headers['proc-type']?.includes('ENCRYPTED')) return der;
  const [alg, ivHex] = (block.headers['dek-info'] ?? '').split(',').map((x) => x.trim());
  const keyLen = (({ 'AES-128-CBC': 16, 'AES-192-CBC': 24, 'AES-256-CBC': 32 }) as Record<string, number>)[alg];
  if (!keyLen || !/^[0-9A-Fa-f]{32}$/.test(ivHex ?? '')) throw new SshKeyError('unsupported_cipher', `PEM keys encrypted with ${alg || 'this cipher'} are not supported.`);
  if (!passphrase) throw new SshKeyError('passphrase_required', 'This key is protected by a passphrase.');
  const iv = fromHex(ivHex);
  // EVP_BytesToKey(MD5, salt = iv[0..8], count 1).
  const pass = utf8(passphrase);
  const salt = iv.subarray(0, 8);
  let key = new Uint8Array(0) as Bytes;
  let prev = new Uint8Array(0) as Bytes;
  while (key.length < keyLen) {
    prev = md5(concat(prev, pass, salt));
    key = concat(key, prev);
  }
  wipe(pass);
  try {
    const k = await crypto.subtle.importKey('raw', key.slice(0, keyLen), 'AES-CBC', false, ['decrypt']);
    const plain = new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-CBC', iv }, k, der));
    asn1.parse(plain); // a wrong passphrase almost never yields valid DER
    return plain;
  } catch {
    throw new SshKeyError('bad_passphrase', 'Wrong passphrase.');
  } finally {
    wipe(key);
  }
}

const OID_PBES2 = '1.2.840.113549.1.5.13';
const OID_PBKDF2 = '1.2.840.113549.1.5.12';
const PRFS: Record<string, string> = {
  '1.2.840.113549.2.7': 'SHA-1',
  '1.2.840.113549.2.9': 'SHA-256',
  '1.2.840.113549.2.10': 'SHA-384',
  '1.2.840.113549.2.11': 'SHA-512',
};
const AES_CBC: Record<string, number> = {
  '2.16.840.1.101.3.4.1.2': 16,
  '2.16.840.1.101.3.4.1.22': 24,
  '2.16.840.1.101.3.4.1.42': 32,
};

async function decryptPkcs8(der: Bytes, passphrase: string): Promise<Bytes> {
  const top = asn1.seq(asn1.parse(der));
  const alg = asn1.seq(top[0]);
  if (asn1.oid(alg[0]) !== OID_PBES2) throw new SshKeyError('unsupported_cipher', 'Only PBES2 (PBKDF2 + AES) encrypted PKCS#8 keys are supported.');
  const params = asn1.seq(alg[1]);
  const kdf = asn1.seq(params[0]);
  const enc = asn1.seq(params[1]);
  if (asn1.oid(kdf[0]) !== OID_PBKDF2) throw new SshKeyError('unsupported_cipher', 'Only PBKDF2 key derivation is supported.');
  const kp = asn1.seq(kdf[1]);
  const salt = asn1.expect(kp[0], asn1.OCTET_STRING).value.slice();
  const iterations = asn1.smallInt(kp[1]);
  let prf = 'SHA-1';
  for (const extra of kp.slice(2)) {
    if (extra.tag === asn1.SEQUENCE) {
      prf = PRFS[asn1.oid(extra.children![0])] ?? '';
      if (!prf) throw new SshKeyError('unsupported_cipher', 'Unsupported PBKDF2 hash.');
    }
  }
  const keyLen = AES_CBC[asn1.oid(enc[0])];
  if (!keyLen) throw new SshKeyError('unsupported_cipher', 'Only AES-CBC encrypted PKCS#8 keys are supported.');
  const iv = asn1.expect(enc[1], asn1.OCTET_STRING).value.slice();
  const data = asn1.expect(top[1], asn1.OCTET_STRING).value.slice();
  if (!passphrase) throw new SshKeyError('passphrase_required', 'This key is protected by a passphrase.');
  if (iterations < 1 || iterations > 10_000_000) throw new SshKeyError('corrupt_key', 'Unreasonable PBKDF2 iterations.');
  const pass = utf8(passphrase);
  const base = await crypto.subtle.importKey('raw', pass, 'PBKDF2', false, ['deriveBits']);
  wipe(pass);
  const bits = new Uint8Array(await crypto.subtle.deriveBits({ name: 'PBKDF2', salt, iterations, hash: prf }, base, keyLen * 8));
  try {
    const k = await crypto.subtle.importKey('raw', bits, 'AES-CBC', false, ['decrypt']);
    const plain = new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-CBC', iv }, k, data));
    asn1.parse(plain);
    return plain;
  } catch {
    throw new SshKeyError('bad_passphrase', 'Wrong passphrase.');
  } finally {
    wipe(bits);
  }
}

// ---------------------------------------------------------------- key material → PrivateKey

type Material =
  | { kind: 'ed25519'; seed: Bytes; pub: Bytes }
  | { kind: 'ec'; type: KeyType; curve: string; jwk: string; size: number; hash: string; bits: number; Q: Bytes; d: Bytes }
  | { kind: 'rsa'; n: Bytes; e: Bytes; d: Bytes; p: Bytes; q: Bytes; qi: Bytes; dp: Bytes; dq: Bytes; bits: number };

function ecMaterial(type: KeyType, curveName: string, Q: Bytes, d: Bytes): Material {
  const c = curveBySsh(curveName);
  if (!c || c.type !== type) throw new SshKeyError('unsupported_key_type', `Curve ${curveName} is not supported.`);
  if (Q.length !== 1 + 2 * c.size || Q[0] !== 4) throw new SshKeyError('corrupt_key', 'Damaged EC public point.');
  if (d.length > c.size) throw new SshKeyError('corrupt_key', 'Damaged EC private key.');
  return { kind: 'ec', type, curve: c.ssh, jwk: c.jwk, size: c.size, hash: c.hash, bits: c.bits, Q, d: bigIntToBytes(bytesToBigInt(d), c.size) };
}

function rsaMaterial(n: Bytes, e: Bytes, d: Bytes, p: Bytes, q: Bytes, qi: Bytes): Material {
  const N = bytesToBigInt(n), P = bytesToBigInt(p), Q = bytesToBigInt(q), D = bytesToBigInt(d);
  if (P * Q !== N || N === 0n) throw new SshKeyError('corrupt_key', 'Damaged RSA key.');
  const bits = N.toString(2).length;
  if (bits < 2048) throw new SshKeyError('unsupported_key_type', `RSA keys need at least 2048 bits (this one has ${bits}).`);
  return { kind: 'rsa', n, e, d, p, q, qi, dp: bigIntToBytes(D % (P - 1n)), dq: bigIntToBytes(D % (Q - 1n)), bits };
}

function publicBlob(k: Material): Bytes {
  if (k.kind === 'ed25519') return concat(sshString('ssh-ed25519'), sshString(k.pub));
  if (k.kind === 'ec') return concat(sshString(k.type), sshString(k.curve), sshString(k.Q));
  return concat(sshString('ssh-rsa'), mpint(k.e), mpint(k.n));
}

async function build(k: Material, comment: string): Promise<PrivateKey> {
  const blob = publicBlob(k);
  const type: KeyType = k.kind === 'ed25519' ? 'ssh-ed25519' : k.kind === 'ec' ? k.type : 'ssh-rsa';
  const fp = new Uint8Array(await crypto.subtle.digest('SHA-256', blob));
  const sign = await signer(k);
  await selfTest(k, sign);
  return {
    type,
    bits: k.kind === 'ed25519' ? 256 : k.bits,
    comment,
    publicKey: `${type} ${b64encode(blob)}${comment ? ' ' + comment.replace(/[\r\n]/g, ' ') : ''}`,
    fingerprint: 'SHA256:' + b64encode(fp).replace(/=+$/, ''),
    sign,
  };
}

async function signer(k: Material): Promise<(data: Uint8Array) => Promise<Bytes>> {
  const s = crypto.subtle;
  if (k.kind === 'ed25519') {
    let native: CryptoKey | null = null;
    try {
      // PKCS#8 wrapping of the 32-byte seed (RFC 8410).
      const pkcs8 = concat(fromHex('302e020100300506032b657004220420'), k.seed);
      native = await s.importKey('pkcs8', pkcs8, { name: 'Ed25519' }, false, ['sign']);
      wipe(pkcs8);
    } catch {
      native = null; // browser without WebCrypto Ed25519: @noble/ed25519 below
    }
    const seed = k.seed;
    return async (data) => {
      let sig: Uint8Array;
      if (native) {
        sig = new Uint8Array(await s.sign({ name: 'Ed25519' }, native, data as Bytes));
      } else {
        sig = await ed.signAsync(data, seed);
      }
      return concat(sshString('ssh-ed25519'), sshString(sig));
    };
  }
  if (k.kind === 'ec') {
    const jwk: JsonWebKey = { kty: 'EC', crv: k.jwk, d: b64url(k.d), x: b64url(k.Q.subarray(1, 1 + k.size)), y: b64url(k.Q.subarray(1 + k.size)), ext: false };
    let key: CryptoKey;
    try {
      key = await s.importKey('jwk', jwk, { name: 'ECDSA', namedCurve: k.jwk }, false, ['sign']);
    } catch (e) {
      throw cryptoError(e, `This browser cannot use ${k.jwk} keys.`);
    }
    return async (data) => {
      const raw = new Uint8Array(await s.sign({ name: 'ECDSA', hash: k.hash }, key, data as Bytes));
      const r = raw.subarray(0, k.size);
      const sv = raw.subarray(k.size);
      return concat(sshString(k.type), sshString(concat(mpint(r), mpint(sv))));
    };
  }
  const jwk: JsonWebKey = {
    kty: 'RSA', n: b64url(k.n), e: b64url(k.e), d: b64url(k.d), p: b64url(k.p), q: b64url(k.q),
    dp: b64url(k.dp), dq: b64url(k.dq), qi: b64url(k.qi), ext: false,
  };
  let key: CryptoKey;
  try {
    key = await s.importKey('jwk', jwk, { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-512' }, false, ['sign']);
  } catch (e) {
    throw cryptoError(e, 'This RSA key could not be loaded.');
  }
  return async (data) => {
    const sig = new Uint8Array(await s.sign('RSASSA-PKCS1-v1_5', key, data as Bytes));
    return concat(sshString('rsa-sha2-512'), sshString(sig));
  };
}

/**
 * Signs a fixed message and verifies it with the public half, so a key whose private part does not
 * match its public part fails here (corrupt_key) instead of being refused by the server.
 */
async function selfTest(k: Material, sign: (data: Uint8Array) => Promise<Bytes>): Promise<void> {
  const msg = utf8('ervisio-ssh-key-self-test');
  const r = new Reader(await sign(msg));
  r.string();
  const sig = r.string();
  let ok = false;
  try {
    if (k.kind === 'ed25519') {
      ok = equal(new Uint8Array(await ed.getPublicKeyAsync(k.seed)), k.pub) && (await ed.verifyAsync(sig, msg, k.pub));
    } else if (k.kind === 'ec') {
      const pub = await crypto.subtle.importKey('raw', k.Q, { name: 'ECDSA', namedCurve: k.jwk }, false, ['verify']);
      const rr = new Reader(sig);
      const raw = concat(bigIntToBytes(bytesToBigInt(rr.string()), k.size), bigIntToBytes(bytesToBigInt(rr.string()), k.size));
      ok = await crypto.subtle.verify({ name: 'ECDSA', hash: k.hash }, pub, raw, msg);
    } else {
      const pub = await crypto.subtle.importKey('jwk', { kty: 'RSA', n: b64url(k.n), e: b64url(k.e) }, { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-512' }, false, ['verify']);
      ok = await crypto.subtle.verify('RSASSA-PKCS1-v1_5', pub, sig, msg);
    }
  } catch {
    /* ok stays false */
  }
  if (!ok) throw new SshKeyError('corrupt_key', 'The private key does not match its public key.');
}

function cryptoError(e: unknown, msg: string): SshKeyError {
  const name = (e as Error)?.name;
  if (name === 'NotSupportedError') return new SshKeyError('crypto_unavailable', msg);
  return new SshKeyError('corrupt_key', msg);
}
