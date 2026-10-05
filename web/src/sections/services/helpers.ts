import type { Tone } from '../../ui';
import type { Action, Unit } from './types';

export const short = (name: string) => name.replace(/\.(service|socket|timer)$/, '');

export function stateTone(u: Pick<Unit, 'state' | 'load' | 'sub'>): Tone {
  if (u.state === 'failed') return 'err';
  if (u.state === 'running') return u.sub === 'paused' ? 'warn' : 'ok';
  if (u.state === 'finished') return 'info';
  return 'neutral';
}

/** Key under `state.` for the badge text. */
export function stateKey(u: Pick<Unit, 'state' | 'sub' | 'load' | 'name'>): string {
  if (u.sub === 'paused') return 'paused';
  if (u.load === 'masked') return 'masked';
  if (u.name.endsWith('.timer') && u.state === 'running') return 'waiting';
  if (u.name.endsWith('.socket') && u.state === 'running') return 'listening';
  return u.state;
}

/** enabled / enabled-runtime can be switched off; disabled can be switched on. Everything else is read-only. */
export function bootMode(enabled: string): 'on' | 'off' | 'fixed' {
  if (enabled === 'enabled' || enabled === 'enabled-runtime') return 'on';
  if (enabled === 'disabled') return 'off';
  return 'fixed';
}

/** Units whose stopping can cut the user off from the machine. */
const CRITICAL = /^(sshd?|ervisio.*|linuxadmin.*|NetworkManager|systemd-(networkd|resolved|logind|journald|udevd)|dbus.*|polkit|getty.*|systemd-.*)\.(service|socket)$/i;

const WIN_CRITICAL = /^(sshd|ervisio.*|TermService|WinRM|RpcSs|DcomLaunch|Dhcp|Dnscache|EventLog|mpssvc|BFE|LanmanServer|LanmanWorkstation|Winmgmt|Netlogon|SamSs|NlaSvc)$/i;

export function isCritical(name: string): boolean {
  return CRITICAL.test(name) || WIN_CRITICAL.test(name);
}

export function needsConfirm(action: Action, name: string): 'typed' | 'simple' | null {
  if (action === 'stop' || action === 'disable' || action === 'mask') {
    if (isCritical(name)) return 'typed';
    if (action === 'mask') return 'simple';
  }
  return null;
}

export function formatClockTime(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}
