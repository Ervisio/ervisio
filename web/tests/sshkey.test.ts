// SSH-key sign-in crypto tests: `npm --prefix web test`.
//
// Fixtures (tests/fixtures/sshkeys) are throw-away keys made with ssh-keygen for these tests only;
// encrypted ones use the passphrase "correct horse battery". Commands used:
//   ssh-keygen -t ed25519 [-N pass] [-Z aes256-gcm@openssh.com|aes128-ctr|aes256-cbc|aes128-gcm@openssh.com|chacha20-poly1305@openssh.com] [-a 4]
//   ssh-keygen -t ecdsa -b 256|384|521 …;  ssh-keygen -t rsa -b 2048|1024 …
//   ssh-keygen -p -m PEM|PKCS8 [-N pass] -f <copy>     (PKCS#1 / SEC1 / PKCS#8, plain or encrypted)
// fingerprints.json = `ssh-keygen -lf <key>.pub`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createPublicKey, verify as nodeVerify } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import * as ed from '@noble/ed25519';
import { bcryptHash, bcryptPbkdf } from '../src/auth/sshkey/bcrypt.ts';
import { md5 } from '../src/auth/sshkey/md5.ts';
import { b64decode, b64url, hex, Reader, utf8, concat } from '../src/auth/sshkey/bytes.ts';
import { needsPassphrase, parsePrivateKey, publicInfo } from '../src/auth/sshkey/keys.ts';
import { SshKeyError } from '../src/auth/sshkey/errors.ts';
import { challengeMessage, signInWithKeyUsing, toSshKeyError, type Transport } from '../src/auth/sshkey/signin.ts';

const dir = fileURLToPath(new URL('./fixtures/sshkeys/', import.meta.url));
const read = (name: string) => readFileSync(dir + name, 'utf8');
const PASS = 'correct horse battery';
const fingerprints: Record<string, string> = JSON.parse(read('fingerprints.json'));

async function rejectsWith(p: Promise<unknown>, code: string) {
  await assert.rejects(p, (e: unknown) => {
    assert.ok(e instanceof SshKeyError, `not an SshKeyError: ${e}`);
    assert.equal(e.code, code, e.message);
    return true;
  });
}

// ---- bcrypt_pbkdf: vectors from OpenBSD's reference implementation (as in golang.org/x/crypto) ----

test('bcrypt_hash vector', () => {
  const pass = new Uint8Array(64).map((_, i) => i);
  const salt = new Uint8Array(64).map((_, i) => i + 64);
  assert.equal(hex(bcryptHash(pass, salt)), '87904870eef9deddf8e7611a140106e6aaf1a363d9a2c504db356443721eb555');
});

test('bcrypt_pbkdf vectors', async () => {
  assert.equal(hex(await bcryptPbkdf(utf8('password'), utf8('salt'), 12, 32)),
    '1ae42c05d487bc02f64921a4ebe4ea93bcacfe135fda99974c06b7b01fae149a');
  assert.equal(hex(await bcryptPbkdf(utf8('passwordy\0PASSWORD\0'), utf8('salty\0SALT\0'), 3, 32)),
    '7f310bd3e78c3280c59ce4595211a2928e8d4ec744c1ed2efc9f764e3388e0ad');
  assert.equal(hex(await bcryptPbkdf(utf8('секретное слово'), utf8('посолить немножко'), 8, 88)),
    '8df43fc6fe131fc47f0c9e39224bd94c70b6fcc8ee8135faddf61156e6cb2733ea765f315a3e1e4afc35bf8687d189254c1e05a6fe80c0617f9183d67260d6a115c6c94e3603e2303fbb43a76a64523ffda686b1d4518543');
});

test('md5 vectors (RFC 1321)', () => {
  assert.equal(hex(md5(utf8(''))), 'd41d8cd98f00b204e9800998ecf8427e');
  assert.equal(hex(md5(utf8('abc'))), '900150983cd24fb0d6963f7d28e17f72');
  assert.equal(hex(md5(utf8('12345678901234567890123456789012345678901234567890123456789012345678901234567890'))), '57edf4a22be3c955ac49da2e2107b67a');
});

// ---- parsing every fixture ----

const fixtures: [name: string, encrypted: boolean][] = [
  ['ed25519', false], ['ed25519-enc', true], ['ed25519-gcm', true], ['ed25519-gcm128', true], ['ed25519-aes128ctr', true], ['ed25519-cbc', true],
  ['ecdsa256', false], ['ecdsa256-enc', true], ['ecdsa384', false], ['ecdsa384-enc', true], ['ecdsa521', false], ['ecdsa521-enc', true],
  ['rsa', false], ['rsa-enc', true],
  ['rsa-pem', false], ['rsa-pem-enc', true], ['rsa-pkcs8', false], ['rsa-pkcs8-enc', true],
  ['ecdsa256-pem', false], ['ecdsa384-pem-enc', true], ['ecdsa521-pkcs8', false], ['ecdsa256-pkcs8-enc', true], ['ed25519-pkcs8', false],
];

/** Verifies an SSH signature blob with node:crypto and the public key from the .pub file (independent of our signing code). */
function verifyWithNode(pubLine: string, msg: Uint8Array, sigBlob: Uint8Array): boolean {
  const [type, b64] = pubLine.trim().split(/\s+/);
  const pr = new Reader(b64decode(b64));
  assert.equal(pr.text(), type);
  const sr = new Reader(sigBlob as any);
  const format = sr.text();
  const sig = sr.string();
  assert.equal(sr.left, 0);
  if (type === 'ssh-ed25519') {
    assert.equal(format, 'ssh-ed25519');
    const key = createPublicKey({ key: { kty: 'OKP', crv: 'Ed25519', x: b64url(pr.string()) }, format: 'jwk' });
    return nodeVerify(null, msg, key, sig);
  }
  if (type === 'ssh-rsa') {
    assert.equal(format, 'rsa-sha2-512');
    const e = pr.string();
    const n = pr.string();
    const key = createPublicKey({ key: { kty: 'RSA', n: b64url(n.subarray(n[0] === 0 ? 1 : 0)), e: b64url(e) }, format: 'jwk' });
    return nodeVerify('sha512', msg, key, sig);
  }
  assert.equal(format, type);
  const curve = pr.text();
  const Q = pr.string();
  const size = { nistp256: 32, nistp384: 48, nistp521: 66 }[curve]!;
  const crv = { nistp256: 'P-256', nistp384: 'P-384', nistp521: 'P-521' }[curve]!;
  const hash = { nistp256: 'sha256', nistp384: 'sha384', nistp521: 'sha512' }[curve]!;
  const key = createPublicKey({ key: { kty: 'EC', crv, x: b64url(Q.subarray(1, 1 + size)), y: b64url(Q.subarray(1 + size)) }, format: 'jwk' });
  const rs = new Reader(sig);
  const pad = (b: Uint8Array) => {
    const v = b[0] === 0 ? b.subarray(1) : b;
    return concat(new Uint8Array(size - v.length), v);
  };
  const raw = concat(pad(rs.string()), pad(rs.string()));
  return nodeVerify(hash, msg, { key, dsaEncoding: 'ieee-p1363' }, raw);
}

for (const [name, encrypted] of fixtures) {
  test(`parse + sign: ${name}`, async () => {
    const text = read(name);
    assert.equal(needsPassphrase(text), encrypted);
    if (encrypted) await rejectsWith(parsePrivateKey(text), 'passphrase_required');
    const k = await parsePrivateKey(text, encrypted ? PASS : '');
    const pub = read(name + '.pub').trim();
    const [ptype, pb64] = pub.split(/\s+/);
    assert.equal(k.type, ptype);
    assert.equal(k.publicKey.split(' ').slice(0, 2).join(' '), `${ptype} ${pb64}`);
    assert.equal(k.fingerprint, fingerprints[name]);
    if (!name.includes('pem') && !name.includes('pkcs8')) assert.equal(k.comment, pub.split(/\s+/)[2]);
    const msg = utf8(challengeMessage('host:9090', 'alice', 'n'.repeat(43)));
    const sig = await k.sign(msg);
    assert.ok(verifyWithNode(pub, msg, sig), 'signature does not verify');
    assert.ok(!verifyWithNode(pub, utf8('other'), sig), 'signature verifies other data');
  });
}

test('wrong passphrase is reported as bad_passphrase', async () => {
  for (const name of ['ed25519-enc', 'ed25519-gcm', 'ed25519-cbc', 'ecdsa256-enc', 'rsa-pem-enc', 'rsa-pkcs8-enc', 'ecdsa384-pem-enc', 'ecdsa256-pkcs8-enc']) {
    await rejectsWith(parsePrivateKey(read(name), 'wrong passphrase'), 'bad_passphrase');
  }
});

test('unsupported and invalid inputs', async () => {
  await rejectsWith(parsePrivateKey(read('ed25519-chacha'), PASS), 'unsupported_cipher');
  await rejectsWith(parsePrivateKey(read('rsa1024')), 'unsupported_key_type');
  await rejectsWith(parsePrivateKey(read('ed25519.pub')), 'public_key_given');
  await rejectsWith(parsePrivateKey(''), 'not_a_key');
  await rejectsWith(parsePrivateKey('hello'), 'not_a_key');
  await rejectsWith(parsePrivateKey('PuTTY-User-Key-File-3: ssh-ed25519\nEncryption: none\n'), 'unsupported_format');
  await rejectsWith(parsePrivateKey('-----BEGIN DSA PRIVATE KEY-----\nAAAA\n-----END DSA PRIVATE KEY-----'), 'unsupported_key_type');
  // Damaged body: a flipped byte in the public key part.
  const t = read('ed25519').split('\n');
  const body = b64decode(t.slice(1, -2).join(''));
  body[60] ^= 1;
  const broken = `-----BEGIN OPENSSH PRIVATE KEY-----\n${Buffer.from(body).toString('base64')}\n-----END OPENSSH PRIVATE KEY-----\n`;
  await assert.rejects(parsePrivateKey(broken), (e: unknown) => e instanceof SshKeyError && ['corrupt_key', 'unsupported_key_type'].includes(e.code));
  // Windows line endings and surrounding text are fine.
  const crlf = 'my key:\r\n' + read('ed25519').replace(/\n/g, '\r\n') + '\r\n';
  assert.equal((await parsePrivateKey(crlf)).type, 'ssh-ed25519');
});

test('ed25519: @noble/ed25519 fallback gives the same signature as WebCrypto', async () => {
  const k = await parsePrivateKey(read('ed25519'));
  const msg = utf8('deterministic');
  const blob = await k.sign(msg);
  const r = new Reader(blob);
  r.string();
  const sig = r.string();
  // The OpenSSH file holds the seed in its private section; recover it through the PKCS#8 copy.
  const p8 = read('ed25519-pkcs8');
  const der = b64decode(p8.split('\n').filter((l) => l && !l.startsWith('-----')).join(''));
  const seed = der.subarray(der.length - 32);
  assert.equal(hex(await ed.signAsync(msg, seed)), hex(sig));
});

// ---- the flow, with a fake server ----

function fakeServer(opts: { challenge?: (b: any) => any; login?: (b: any) => any } = {}) {
  const calls: { path: string; body: any }[] = [];
  const nonce = 'A'.repeat(43);
  const t: Transport = {
    async post<T>(path: string, body: any): Promise<T> {
      calls.push({ path, body });
      if (path === '/api/auth/challenge') {
        return (opts.challenge?.(body) ?? { nonce, host: body.host, challenge: challengeMessage(body.host, body.user, nonce), expires: Date.now() + 60_000 }) as T;
      }
      return (opts.login?.(body) ?? { user: body.user, isAdmin: false, isRoot: false, authMethod: 'ssh-key' }) as T;
    },
  };
  return { t, calls, nonce };
}

test('signInWithKey sends only the public key and a valid signature', async () => {
  const { t, calls, nonce } = fakeServer();
  const res = await signInWithKeyUsing(t, { user: 'alice', keyText: read('ecdsa256-enc'), passphrase: PASS, stay: false, host: 'srv:9090' });
  assert.equal(res.user, 'alice');
  assert.deepEqual(calls[0], { path: '/api/auth/challenge', body: { user: 'alice', host: 'srv:9090' } });
  const login = calls[1].body;
  assert.equal(calls[1].path, '/api/auth/login-key');
  assert.deepEqual(Object.keys(login).sort(), ['nonce', 'publicKey', 'remember', 'signature', 'user']);
  assert.equal(login.nonce, nonce);
  assert.equal(login.remember, false);
  const sent = JSON.stringify(calls);
  assert.ok(!sent.includes(PASS) && !sent.includes('PRIVATE KEY'), 'secret material sent');
  const pub = read('ecdsa256-enc.pub');
  assert.ok(verifyWithNode(pub, utf8(challengeMessage('srv:9090', 'alice', nonce)), b64decode(login.signature)));
});

test('signInWithKey refuses to sign a challenge it did not build', async () => {
  const { t, calls } = fakeServer({ challenge: (b) => ({ nonce: 'B'.repeat(43), challenge: `something else\n${b.user}` }) });
  await rejectsWith(signInWithKeyUsing(t, { user: 'alice', keyText: read('ed25519'), host: 'srv' }), 'server_error');
  assert.equal(calls.length, 1);
});

test('local key errors happen before any request', async () => {
  const { t, calls } = fakeServer();
  await rejectsWith(signInWithKeyUsing(t, { user: 'alice', keyText: read('rsa-enc'), passphrase: 'nope', host: 'h' }), 'bad_passphrase');
  assert.equal(calls.length, 0);
});

test('server errors map to codes', async () => {
  const err = (status: number, code: string, data?: object) => Object.assign(new Error('x'), { status, code, data });
  assert.equal(toSshKeyError(err(401, 'unauthenticated', { reason: 'key_refused' })).code, 'key_refused');
  assert.equal(toSshKeyError(err(401, 'unauthenticated', { reason: 'challenge_invalid' })).code, 'challenge_invalid');
  assert.equal(toSshKeyError(err(403, 'forbidden', { reason: 'ssh_keys_disabled' })).code, 'ssh_keys_disabled');
  assert.equal(toSshKeyError(err(400, 'invalid', { reason: 'host_not_allowed' })).code, 'host_not_allowed');
  assert.equal(toSshKeyError(err(400, 'invalid', { reason: 'unsupported_key' })).code, 'unsupported_key_type');
  const rl = toSshKeyError(err(429, 'forbidden', { reason: 'rate_limited', retryAfter: 600 }));
  assert.equal(rl.code, 'rate_limited');
  assert.equal(rl.retryAfter, 600);
  assert.equal(toSshKeyError(err(0, 'network')).code, 'network');
  assert.equal(toSshKeyError(err(503, 'unavailable')).code, 'server_error');
  const { t } = fakeServer({ login: () => { throw err(401, 'unauthenticated', { reason: 'key_refused' }); } });
  await rejectsWith(signInWithKeyUsing(t, { user: 'alice', keyText: read('ed25519'), host: 'h' }), 'key_refused');
});

test('ed25519 keys still sign when WebCrypto lacks Ed25519', async () => {
  const subtle = crypto.subtle;
  const orig = subtle.importKey;
  (subtle as any).importKey = function (format: any, data: any, alg: any, ...rest: any[]) {
    if ((alg?.name ?? alg) === 'Ed25519') return Promise.reject(Object.assign(new Error('no'), { name: 'NotSupportedError' }));
    return (orig as any).call(subtle, format, data, alg, ...rest);
  };
  try {
    const k = await parsePrivateKey(read('ed25519'));
    const msg = utf8('fallback');
    assert.ok(verifyWithNode(read('ed25519.pub'), msg, await k.sign(msg)));
  } finally {
    (subtle as any).importKey = orig;
  }
});

test('publicInfo reads type and fingerprint of OpenSSH keys without the passphrase', async () => {
  const dir = fileURLToPath(new URL('./fixtures/sshkeys/', import.meta.url));
  const fps = JSON.parse(readFileSync(dir + 'fingerprints.json', 'utf8')) as Record<string, string>;
  for (const name of ['ed25519-enc', 'ecdsa384-enc', 'ecdsa521-enc', 'ed25519']) {
    const info = await publicInfo(readFileSync(dir + name, 'utf8'));
    assert.equal(info?.fingerprint, fps[name], name);
  }
  assert.equal((await publicInfo(readFileSync(dir + 'ecdsa384-enc', 'utf8')))?.bits, 384);
  assert.equal(await publicInfo(readFileSync(dir + 'ecdsa256-pem', 'utf8')), null);
  assert.equal(await publicInfo('garbage'), null);
});

test('publicInfo gives the RSA modulus size', async () => {
  const dir = fileURLToPath(new URL('./fixtures/sshkeys/', import.meta.url));
  assert.equal((await publicInfo(readFileSync(dir + 'rsa1024', 'utf8')))?.bits, 1024);
});
