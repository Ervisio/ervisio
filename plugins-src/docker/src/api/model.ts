/** Helpers over the container list that several views share. */
import { containerName } from './format';
import { COMPOSE_PROJECT, type Container } from './types';

export type Health = 'healthy' | 'unhealthy' | 'starting' | 'none';

export function healthOf(c: Container): Health {
  const m = /\((?:health: )?(healthy|unhealthy|starting)\)/.exec(c.Status);
  return m ? (m[1] as Health) : 'none';
}

/** Needs attention: unhealthy, or restarting (a restart loop). */
export const isProblem = (c: Container): boolean => healthOf(c) === 'unhealthy' || c.State === 'restarting';
export const isStopped = (c: Container): boolean => ['exited', 'created', 'dead'].includes(c.State);
export const isUp = (c: Container): boolean => c.State === 'running' || c.State === 'restarting' || c.State === 'paused';

export const stackOf = (c: Container): string => c.Labels?.[COMPOSE_PROJECT] ?? '';

export interface StackGroup {
  /** '' for containers that belong to no compose project. */
  name: string;
  containers: Container[];
  running: number;
}

/** Groups by compose project (alphabetical), standalone containers last. Containers keep name order. */
export function groupByStack(list: Container[]): StackGroup[] {
  const map = new Map<string, Container[]>();
  for (const c of [...list].sort((a, b) => containerName(a).localeCompare(containerName(b)))) {
    const k = stackOf(c);
    if (!map.has(k)) map.set(k, []);
    map.get(k)!.push(c);
  }
  return [...map.entries()]
    .sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b)))
    .map(([name, containers]) => ({ name, containers, running: containers.filter((c) => c.State === 'running').length }));
}

export interface PublishedPort {
  host: number;
  container: number;
  proto: string;
}

/** Published TCP/UDP ports once each (the Engine lists IPv4 and IPv6 separately). */
export function publishedPorts(c: Container): PublishedPort[] {
  const seen = new Set<string>();
  const out: PublishedPort[] = [];
  for (const p of c.Ports ?? []) {
    if (!p.PublicPort) continue;
    const k = `${p.PublicPort}/${p.Type}`;
    if (seen.has(k)) continue;
    seen.add(k);
    out.push({ host: p.PublicPort, container: p.PrivatePort, proto: p.Type });
  }
  return out.sort((a, b) => a.host - b.host);
}

export interface Counts {
  total: number;
  running: number;
  restarting: number;
  paused: number;
  stopped: number;
  problems: number;
}

export function countContainers(list: Container[]): Counts {
  return {
    total: list.length,
    running: list.filter((c) => c.State === 'running').length,
    restarting: list.filter((c) => c.State === 'restarting').length,
    paused: list.filter((c) => c.State === 'paused').length,
    stopped: list.filter(isStopped).length,
    problems: list.filter(isProblem).length,
  };
}

/** Case-insensitive match of a search text against what users look for in a container. */
export function matchesContainer(c: Container, q: string): boolean {
  const s = q.trim().toLowerCase();
  if (!s) return true;
  return [containerName(c), c.Image, stackOf(c), c.Id.slice(0, 12)].some((x) => x.toLowerCase().includes(s));
}

/** Where a published port can be opened from this browser: the host name the app is served from. */
export function portUrl(p: PublishedPort): string {
  const host = (typeof location !== 'undefined' && location.hostname) || 'localhost';
  const scheme = p.container === 443 || p.container === 8443 ? 'https' : 'http';
  return `${scheme}://${host}:${p.host}`;
}
