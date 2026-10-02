/**
 * Sign in with an SSH key. Public entry point for the UI.
 *
 *   import { signInWithKey, needsPassphrase, describeKey, SshKeyError } from '../auth/sshkey';
 *
 * Contract (see docs/api/auth.md, "Sign in with an SSH key"):
 * - `needsPassphrase(keyText)`  → boolean, synchronous, never throws: show the passphrase field.
 * - `describeKey(keyText, passphrase?)` → { type, bits, comment, publicKey, fingerprint }; optional
 *   preview before signing in. Throws SshKeyError.
 * - `signInWithKey({ user, keyText, passphrase?, stay? })` → same object as the password sign-in
 *   (user, isAdmin, isRoot, authMethod: "ssh-key", keyFingerprint). Throws SshKeyError whose `code`
 *   is one of SshKeyErrorCode (errors.ts); `retryAfter` is set for rate_limited / busy.
 *   Prefer `useSession().signInWithKey(...)`, which also updates the session state.
 *
 * The private key and passphrase stay in the page: only the public key and a signature over
 * "linuxadmin-ssh-auth-v1\n<host>\n<user>\n<nonce>" are sent.
 */
import { authChallenge, loginKey } from '../../api/client';
import { signInWithKeyUsing, type KeySignInResult, type SignInWithKeyArgs, type Transport } from './signin.ts';

export { SshKeyError, type SshKeyErrorCode } from './errors.ts';
export { needsPassphrase, publicInfo, type KeyType, type PrivateKey } from './keys.ts';
export { describeKey, challengeMessage, type KeySignInResult, type SignInWithKeyArgs } from './signin.ts';

const transport: Transport = {
  post<T>(path: string, body: unknown): Promise<T> {
    const b = body as { user: string; host: string } & Parameters<typeof loginKey>[0];
    if (path === '/api/auth/challenge') return authChallenge(b.user, b.host) as Promise<T>;
    return loginKey(b) as Promise<T>;
  },
};

export function signInWithKey(args: SignInWithKeyArgs): Promise<KeySignInResult> {
  return signInWithKeyUsing(transport, args);
}
