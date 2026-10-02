/**
 * The shared stores every view can use:
 *   const { data, error, loading } = containers.use();   // Container[]
 *   await containers.refresh();                           // after an action
 * Add new stores with createResource() (see store.ts).
 */
import { docker } from './engine';
import { createResource } from './store';
import type { Container, DiskUsage, ImageSummary, NetworkInfo, SystemInfo, VersionInfo, VolumeInfo } from './types';
import { engineVersion } from './engine';

const typeIs = (...types: string[]) => (ev: { Type: string; Action: string }) =>
  (types.includes(ev.Type) && !ev.Action.startsWith('exec_') && ev.Action !== 'attach') || (ev.Type === 'system' && ev.Action === 'reconnect');

export const containers = createResource<Container[]>(() => docker.get<Container[]>('/containers/json', { all: '1' }), {
  intervalMs: 5000,
  refreshOn: typeIs('container'),
});

export const images = createResource<ImageSummary[]>(() => docker.get<ImageSummary[]>('/images/json', { 'shared-size': '1' }), {
  intervalMs: 20000,
  refreshOn: typeIs('image', 'container'),
});

export const volumes = createResource<VolumeInfo[]>(async () => (await docker.get<{ Volumes: VolumeInfo[] | null }>('/volumes')).Volumes ?? [], {
  intervalMs: 20000,
  refreshOn: typeIs('volume'),
});

export const networks = createResource<NetworkInfo[]>(() => docker.get<NetworkInfo[]>('/networks'), {
  intervalMs: 20000,
  refreshOn: typeIs('network'),
});

export const info = createResource<{ info: SystemInfo; version: VersionInfo }>(
  async () => ({ info: await docker.get<SystemInfo>('/info'), version: await engineVersion() }),
  { intervalMs: 30000 },
);

/** /system/df can take seconds on big hosts: polled slowly, refreshed by image/volume/container events. */
export const diskUsage = createResource<DiskUsage>(() => docker.get<DiskUsage>('/system/df'), {
  intervalMs: 60000,
  refreshOn: (ev) => ['image', 'volume'].includes(ev.Type) && ['delete', 'prune', 'pull', 'destroy', 'create'].includes(ev.Action),
});
