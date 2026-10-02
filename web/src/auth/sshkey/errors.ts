/**
 * Error codes of SSH-key sign-in (SshKeyError.code). The UI picks its message from the code;
 * `message` is an English fallback.
 *
 * Local (the key never left the browser):
 * - `not_a_key`            the text is not a private key we recognise (also: empty input)
 * - `public_key_given`     a public key (.pub / authorized_keys line) was given instead of the private key
 * - `unsupported_format`   a private key in a format we do not read (PuTTY .ppk, SSH2/RFC4716 private…)
 * - `passphrase_required`  the key is encrypted and no passphrase was given
 * - `bad_passphrase`       the passphrase is wrong
 * - `unsupported_key_type` DSA, FIDO (sk-*), RSA < 2048 bits, an unknown curve…
 * - `unsupported_cipher`   the key is encrypted with a cipher we cannot decrypt (chacha20-poly1305, 3des…)
 * - `corrupt_key`          the key data is damaged or inconsistent
 * - `crypto_unavailable`   WebCrypto is missing (page not served over https or localhost) or lacks an algorithm
 *
 * From the server:
 * - `key_refused`          the key is not accepted for this user (not in authorized_keys, its options refuse
 *                          this sign-in, unknown user, account locked/expired, shell not allowed). Deliberately
 *                          one code: the server never says whether the user exists.
 * - `challenge_invalid`    the challenge expired (60 s) or was used; retry from the start
 * - `ssh_keys_disabled`    the server has auth.ssh_keys = false
 * - `root_disabled`        user "root" with allow_root = false
 * - `dev_mode_user`        --dev only allows the daemon's own user
 * - `host_not_allowed`     the server does not answer to the host name in the address bar (web.allowed_origins)
 * - `rate_limited`         too many failed attempts (password and key failures share the limit); see retryAfter
 * - `busy`                 another attempt from this client is running, or the server is busy; retry shortly
 * - `network`              the server could not be reached
 * - `server_error`         anything else (session could not start, PAM error…)
 */
export type SshKeyErrorCode =
  | 'not_a_key'
  | 'public_key_given'
  | 'unsupported_format'
  | 'passphrase_required'
  | 'bad_passphrase'
  | 'unsupported_key_type'
  | 'unsupported_cipher'
  | 'corrupt_key'
  | 'crypto_unavailable'
  | 'key_refused'
  | 'challenge_invalid'
  | 'ssh_keys_disabled'
  | 'root_disabled'
  | 'dev_mode_user'
  | 'host_not_allowed'
  | 'rate_limited'
  | 'busy'
  | 'network'
  | 'server_error';

export class SshKeyError extends Error {
  code: SshKeyErrorCode;
  /** Seconds to wait (rate_limited / busy), when the server said so. */
  retryAfter?: number;
  constructor(code: SshKeyErrorCode, message: string, retryAfter?: number) {
    super(message);
    this.name = 'SshKeyError';
    this.code = code;
    this.retryAfter = retryAfter;
  }
}
