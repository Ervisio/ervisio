import { call } from '../../api';

export type EnvKind = 'tcp-tls' | 'ssh' | 'portainer-agent' | 'ervisio';
export const ENV_KINDS: EnvKind[] = ['tcp-tls', 'ssh', 'portainer-agent', 'ervisio'];

export interface EnvStatus {
  reachable: boolean;
  engineVersion?: string;
  apiVersion?: string;
  latencyMs: number;
  error?: string;
  checked: string;
}

export interface EnvAccess {
  mode: 'all' | 'restricted';
  users: string[];
  groups: string[];
}

/** An environment as the daemon shows it: public fields and which secrets are set, never the secrets. */
export interface EnvView {
  id: string;
  name: string;
  kind: EnvKind;
  created: string;
  access: EnvAccess;
  address: string;
  user?: string;
  socketPath?: string;
  insecure?: boolean;
  skipVerify?: boolean;
  ca?: string;
  clientCert?: string;
  fingerprint?: string;
  hostKey?: string;
  pairedWith?: string;
  hasSecrets: Record<string, boolean>;
  status?: EnvStatus;
}

export interface Pairing {
  id: string;
  name: string;
  user: string;
  createdBy: string;
  created: string;
  lastUsed: string;
  lastVia?: string;
  lastAddr?: string;
}

export interface EnvList {
  envs: EnvView[];
  pairings: Pairing[];
  openTokens: number;
  server: string;
}

export interface ProbeResult {
  fingerprint: string;
  hostKey?: string;
  kind: EnvKind;
  plain?: boolean;
}

export interface PairToken {
  token: string;
  expires: string;
  user: string;
  fingerprint: string;
  server: string;
  ttlMinutes: number;
}

/** What the form sends (the daemon's envs.Input). Secrets are only sent when typed. */
export interface EnvInput {
  name: string;
  kind: EnvKind;
  address: string;
  user?: string;
  socketPath?: string;
  insecure?: boolean;
  skipVerify?: boolean;
  ca?: string;
  clientCert?: string;
  fingerprint?: string;
  hostKey?: string;
  access?: EnvAccess;
  clientKey?: string;
  sshKey?: string;
  passphrase?: string;
  agentSecret?: string;
  token?: string;
}

export const envsList = () => call<EnvList>('envs.list');
export const envsProbe = (i: Pick<EnvInput, 'kind' | 'address' | 'user' | 'insecure'>) => call<ProbeResult>('envs.probe', i);
export const envsCreate = (i: EnvInput) => call<EnvView>('envs.create', i);
export const envsUpdate = (id: string, i: EnvInput) => call<EnvView>('envs.update', { id, ...i });
export const envsDelete = (id: string) => call('envs.delete', { id });
export const envsTest = (id: string) => call<EnvStatus>('envs.test', { id });
export const pairTokenCreate = (user?: string) => call<PairToken>('envs.pairToken.create', user ? { user } : {});
export const pairingRevoke = (id: string) => call('envs.pairings.revoke', { id });

export interface ApprovedHost {
  plugin: string;
  host: string;
  scheme: 'https' | 'http';
  by: string;
  at: string;
}
export const hostsList = () => call<{ approved: ApprovedHost[] }>('plugins.network.list');
export const hostRevoke = (plugin: string, host: string) => call('plugins.network.revoke', { plugin, host }, { admin: true });
