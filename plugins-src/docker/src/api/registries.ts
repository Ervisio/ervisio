/**
 * Stored registry logins (registries.json) and the X-Registry-Auth header Docker wants on pulls.
 *
 *   const auth = await registryAuthFor('ghcr.io/org/app:1.2');   // base64url JSON, or undefined without a login
 *   docker.stream('POST', '/images/create', { query: { fromImage }, headers: auth ? { 'X-Registry-Auth': auth } : undefined }, ...)
 */
import { docker } from './engine';
import { loadFile, saveFile, type Registry } from '../settings';

export type { Registry } from '../settings';

export const DOCKER_HUB = 'docker.io';
const HUB_ALIASES = new Set(['docker.io', 'index.docker.io', 'registry-1.docker.io', 'registry.hub.docker.com']);

/** "https://ghcr.io/" -> "ghcr.io"; every Docker Hub spelling -> "docker.io". */
export function normalizeServer(server: string): string {
  const s = server.trim().toLowerCase().replace(/^[a-z]+:\/\//, '').replace(/\/.*$/, '');
  return HUB_ALIASES.has(s) ? DOCKER_HUB : s;
}

/** The registry host of an image reference: "ghcr.io/org/app:1" -> "ghcr.io", "nginx" -> "docker.io". */
export function registryHost(image: string): string {
  const ref = image.trim().replace(/^[a-z]+:\/\//, '');
  const i = ref.indexOf('/');
  if (i < 0) return DOCKER_HUB;
  const first = ref.slice(0, i);
  if (first.includes('.') || first.includes(':') || first === 'localhost') return normalizeServer(first);
  return DOCKER_HUB;
}

/** What Docker calls the registry in a login: Hub has its own legacy address. */
export const serverAddress = (server: string): string => {
  const s = normalizeServer(server);
  return s === DOCKER_HUB ? 'https://index.docker.io/v1/' : s;
};

function base64url(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = '';
  bytes.forEach((b) => (bin += String.fromCharCode(b)));
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_');
}

/** The X-Registry-Auth value for a stored login (base64url JSON). */
export function authHeader(r: Pick<Registry, 'server' | 'username' | 'password'>): string {
  return base64url(JSON.stringify({ username: r.username, password: r.password, serveraddress: serverAddress(r.server) }));
}

export async function listRegistries(): Promise<Registry[]> {
  return (await loadFile('registries')).registries ?? [];
}

export async function saveRegistries(registries: Registry[]): Promise<void> {
  await saveFile('registries', { registries });
}

/** The stored login for an image's registry, if any. */
export async function registryFor(image: string): Promise<Registry | undefined> {
  const host = registryHost(image);
  return (await listRegistries()).find((r) => normalizeServer(r.server) === host && r.username);
}

/** Base64url JSON for the X-Registry-Auth header of the image's registry, or undefined when no login is stored. */
export async function registryAuthFor(image: string): Promise<string | undefined> {
  const r = await registryFor(image);
  return r ? authHeader(r) : undefined;
}

/** POST /auth: true when the registry accepted the login, otherwise throws with the registry's message. */
export async function testRegistry(r: Pick<Registry, 'server' | 'username' | 'password'>): Promise<void> {
  await docker.post('/auth', undefined, { username: r.username, password: r.password, serveraddress: serverAddress(r.server) });
}
