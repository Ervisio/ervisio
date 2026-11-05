import type { ReactNode } from 'react';
import { ApiError } from '../api';
import { SshKeyError, type SshKeyErrorCode } from '../auth/sshkey';
import type { TFn } from '../i18n';

export type Problem = null | {
  kind: 'key' | 'wrong' | 'root' | 'rate' | 'busy' | 'forbidden' | 'network' | 'other';
  text?: string;
  minutes?: number;
  code?: SshKeyErrorCode;
  seconds?: number;
};

export function classify(e: unknown): Problem {
  if (!(e instanceof ApiError)) return { kind: 'other', text: String(e) };
  const d = (e.data ?? {}) as { reason?: string; retryAfter?: number };
  if (d.reason === 'root_disabled' || (e.code === 'forbidden' && /root/i.test(e.message)))
    return { kind: 'root' };
  if (d.reason === 'busy') return { kind: 'busy' };
  if (e.status === 429 || d.reason === 'rate_limited' || /too many|rate/i.test(e.message))
    return {
      kind: 'rate',
      minutes: d.retryAfter ? Math.max(1, Math.ceil(d.retryAfter / 60)) : undefined,
    };
  if (e.code === 'forbidden') return { kind: 'forbidden', text: e.message };
  if (e.code === 'unauthenticated' || e.code === 'invalid') return { kind: 'wrong' };
  if (e.code === 'network') return { kind: 'network' };
  return { kind: 'other', text: e.message };
}

export function classifyKey(e: unknown): NonNullable<Problem> {
  if (e instanceof SshKeyError) return { kind: 'key', code: e.code, seconds: e.retryAfter };
  return { kind: 'key', code: 'server_error' };
}

/** Message hue and icon per key error code (red = the key or passphrase is wrong, amber = wait or policy). */
const KEY_MSG: Record<SshKeyErrorCode, { icon: string; hue: string }> = {
  not_a_key: { icon: 'alert', hue: 'hue-svc' },
  public_key_given: { icon: 'alert', hue: 'hue-svc' },
  unsupported_format: { icon: 'alert', hue: 'hue-svc' },
  passphrase_required: { icon: 'lock', hue: 'hue-log' },
  bad_passphrase: { icon: 'lock', hue: 'hue-svc' },
  unsupported_key_type: { icon: 'alert', hue: 'hue-svc' },
  unsupported_cipher: { icon: 'alert', hue: 'hue-svc' },
  corrupt_key: { icon: 'alert', hue: 'hue-svc' },
  crypto_unavailable: { icon: 'shield', hue: 'hue-log' },
  key_refused: { icon: 'alert', hue: 'hue-svc' },
  challenge_invalid: { icon: 'clock', hue: 'hue-log' },
  ssh_keys_disabled: { icon: 'lock', hue: 'hue-log' },
  root_disabled: { icon: 'shield', hue: 'hue-log' },
  dev_mode_user: { icon: 'shield', hue: 'hue-log' },
  host_not_allowed: { icon: 'shield', hue: 'hue-log' },
  rate_limited: { icon: 'clock', hue: 'hue-log' },
  busy: { icon: 'clock', hue: 'hue-log' },
  network: { icon: 'alert', hue: 'hue-svc' },
  server_error: { icon: 'alert', hue: 'hue-svc' },
};

export type Message = { icon: string; hue: string; body: ReactNode };

function keyWaitVars(t: TFn, seconds?: number) {
  if (seconds && seconds > 90)
    return { wait: t('key.waitMinutes', { minutes: Math.ceil(seconds / 60) }) };
  if (seconds) return { wait: t('key.waitSeconds', { seconds: Math.ceil(seconds) }) };
  return { wait: t('key.waitLater') };
}

function keyMessage(problem: NonNullable<Problem>, t: TFn): Message {
  const code = problem.code ?? 'server_error';
  const k = KEY_MSG[code];
  return { icon: k.icon, hue: k.hue, body: t(`err.key.${code}`, keyWaitVars(t, problem.seconds)) };
}

function rateBody(problem: NonNullable<Problem>, t: TFn) {
  return problem.minutes ? t('err.rateMinutes', { minutes: problem.minutes }) : t('err.rate');
}

function rootBody(t: TFn) {
  return (
    <>
      {t('err.root.before')} <code>allow_root = true</code> {t('err.root.after')}
    </>
  );
}

export function problemMessage(problem: Problem, t: TFn): Message | null {
  if (!problem) return null;
  switch (problem.kind) {
    case 'key':
      return keyMessage(problem, t);
    case 'wrong':
      return { icon: 'alert', hue: 'hue-svc', body: t('err.wrong') };
    case 'root':
      return { icon: 'shield', hue: 'hue-log', body: rootBody(t) };
    case 'busy':
      return { icon: 'clock', hue: 'hue-log', body: t('err.busy') };
    case 'rate':
      return { icon: 'clock', hue: 'hue-log', body: rateBody(problem, t) };
    case 'forbidden':
      return { icon: 'lock', hue: 'hue-svc', body: problem.text || t('err.forbidden') };
    case 'network':
      return { icon: 'alert', hue: 'hue-svc', body: t('err.network') };
    default:
      return { icon: 'alert', hue: 'hue-svc', body: problem.text || t('err.other') };
  }
}
