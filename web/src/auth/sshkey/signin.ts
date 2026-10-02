// SSH-key sign-in flow, independent of the HTTP layer (tests inject a transport).
import { b64encode, utf8 } from './bytes.ts';
import { SshKeyError, type SshKeyErrorCode } from './errors.ts';
import { parsePrivateKey, type PrivateKey } from './keys.ts';

/** Domain-separation prefix; must match server/internal/sshauth.Prefix. */
export const SIGN_PREFIX = 'ervisio-ssh-auth-v1';

/** The exact text signed: prefix, host, user and nonce, one per line. */
export function challengeMessage(host: string, user: string, nonce: string): string {
  return `${SIGN_PREFIX}\n${host}\n${user}\n${nonce}`;
}

export interface SignInWithKeyArgs {
  user: string;
  /** Private key text (pasted, or read from the chosen file with File.text()). */
  keyText: string;
  /** Passphrase, for encrypted keys. */
  passphrase?: string;
  /** "Stay signed in" (cookie lives for session.timeout). Default true, like signIn. */
  stay?: boolean;
  /** Host name to sign (default location.host). Tests only. */
  host?: string;
}

/** Same shape as the password login result (GET /api/auth/session). */
export interface KeySignInResult {
  user: string;
  isAdmin: boolean;
  isRoot: boolean;
  authMethod?: string;
  keyFingerprint?: string;
}

export interface Transport {
  /** POST JSON; rejects with an object carrying code / status / data (ApiError). */
  post<T>(path: string, body: unknown): Promise<T>;
}

interface ApiLikeError {
  code?: string;
  status?: number;
  message?: string;
  data?: { reason?: string; retryAfter?: number } | null;
}

const SERVER_REASONS: Record<string, SshKeyErrorCode> = {
  key_refused: 'key_refused',
  challenge_invalid: 'challenge_invalid',
  ssh_keys_disabled: 'ssh_keys_disabled',
  root_disabled: 'root_disabled',
  dev_mode_user: 'dev_mode_user',
  host_not_allowed: 'host_not_allowed',
  unsupported_key: 'unsupported_key_type',
  rate_limited: 'rate_limited',
  busy: 'busy',
};

/** Maps an HTTP/API error to an SshKeyError. */
export function toSshKeyError(e: unknown): SshKeyError {
  if (e instanceof SshKeyError) return e;
  const a = (e ?? {}) as ApiLikeError;
  const reason = a.data?.reason ?? '';
  const retry = typeof a.data?.retryAfter === 'number' ? a.data.retryAfter : undefined;
  if (SERVER_REASONS[reason]) return new SshKeyError(SERVER_REASONS[reason], a.message || reason, retry);
  if (a.status === 429) return new SshKeyError('rate_limited', a.message || 'Too many attempts.', retry);
  if (a.code === 'network') return new SshKeyError('network', a.message || 'Cannot reach the server.');
  if (a.code === 'unauthenticated') return new SshKeyError('key_refused', a.message || 'This key is not accepted for this user.');
  return new SshKeyError('server_error', a.message || 'The server could not sign you in.');
}

/**
 * Loads the key and returns what the UI may show before signing in (type, size, fingerprint,
 * comment, public key). Throws SshKeyError (bad_passphrase, passphrase_required, …).
 */
export async function describeKey(keyText: string, passphrase = ''): Promise<Omit<PrivateKey, 'sign'>> {
  const k = await parsePrivateKey(keyText, passphrase);
  return { type: k.type, bits: k.bits, comment: k.comment, publicKey: k.publicKey, fingerprint: k.fingerprint };
}

/**
 * The whole flow: parse/decrypt the key locally, ask the server for a challenge, sign it, send the
 * signature with the public key. The private key and passphrase never leave the browser.
 */
export async function signInWithKeyUsing(t: Transport, args: SignInWithKeyArgs): Promise<KeySignInResult> {
  const user = (args.user ?? '').trim();
  if (!user) throw new SshKeyError('key_refused', 'Enter a user name.');
  const key = await parsePrivateKey(args.keyText, args.passphrase ?? '');
  const host = args.host ?? globalThis.location?.host ?? '';
  let ch: { nonce: string; challenge: string };
  try {
    ch = await t.post('/api/auth/challenge', { user, host });
  } catch (e) {
    throw toSshKeyError(e);
  }
  // Never sign text chosen by the server: only the message built here.
  const msg = challengeMessage(host, user, ch?.nonce ?? '');
  if (!ch || typeof ch.nonce !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(ch.nonce) || ch.challenge !== msg) {
    throw new SshKeyError('server_error', 'The server sent an unexpected challenge.');
  }
  const sig = await key.sign(utf8(msg));
  try {
    return await t.post<KeySignInResult>('/api/auth/login-key', {
      user,
      publicKey: key.publicKey,
      signature: b64encode(sig),
      nonce: ch.nonce,
      remember: args.stay ?? true,
    });
  } catch (e) {
    throw toSshKeyError(e);
  }
}
