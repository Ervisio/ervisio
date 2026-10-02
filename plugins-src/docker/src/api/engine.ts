/**
 * Docker Engine client over sdk.api.http('docker', ...). The API version is negotiated once with GET /version
 * (an unversioned path), then every call goes to /v{ApiVersion}/... Pass paths WITHOUT the version prefix:
 *
 *   const list = await docker.get<Container[]>('/containers/json', { all: '1' });
 *   await docker.post('/containers/abc/start');
 *
 * Non-2xx answers throw DockerError (status + the Engine's own message). Broker errors (needs_admin, unavailable,
 * forbidden) arrive as PluginError with `code`; classify() turns either kind into something to show.
 */
import { getSdk, type HttpRequest, type HttpResponse, type PluginError, type Query } from '../sdk';
import type { VersionInfo } from './types';

export const HTTP_NAME = 'docker';

export class DockerError extends Error {
  status: number;
  code: string;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'DockerError';
    this.status = status;
    this.code = status === 404 ? 'not_found' : status === 409 ? 'conflict' : 'engine';
  }
}

export interface RequestOptions {
  query?: Query;
  headers?: Record<string, string>;
  body?: HttpRequest['body'];
}

let versionPromise: Promise<VersionInfo> | undefined;

/** GET /version once; a failed attempt is retried by the next call. */
export function engineVersion(): Promise<VersionInfo> {
  if (!versionPromise) {
    versionPromise = getSdk()
      .api.http(HTTP_NAME, { method: 'GET', path: '/version' })
      .then((r) => {
        if (r.status >= 400) throw toError(r);
        return r.json() as VersionInfo;
      })
      .catch((e) => {
        versionPromise = undefined;
        throw e;
      });
  }
  return versionPromise;
}

/** Forget the negotiated version (after a reconnect, or when the engine was upgraded). */
export function resetEngine(): void {
  versionPromise = undefined;
}

async function versioned(path: string): Promise<string> {
  const v = await engineVersion();
  return `/v${v.ApiVersion}${path}`;
}

function toError(r: HttpResponse): DockerError {
  let msg = '';
  try {
    msg = (r.json() as { message?: string }).message ?? '';
  } catch {
    msg = r.body?.slice(0, 300) ?? '';
  }
  return new DockerError(r.status, msg || `Docker answered ${r.status}`);
}

/** Raw request: returns the response even for 4xx/5xx. */
export async function request(method: string, path: string, opts: RequestOptions = {}): Promise<HttpResponse> {
  return getSdk().api.http(HTTP_NAME, { method, path: await versioned(path), ...opts });
}

/** Request that throws DockerError for non-2xx and returns the decoded JSON body (or undefined when empty). */
export async function json<T = unknown>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const r = await request(method, path, opts);
  if (r.status >= 400) throw toError(r);
  if (!r.body) return undefined as T;
  try {
    return r.json() as T;
  } catch {
    return r.body as unknown as T;
  }
}

export interface StreamHandle {
  close(): void;
}

export interface StreamHandlers {
  onStart?(status: number, headers: Record<string, string>): void;
  onData(chunk: Uint8Array): void;
  onEnd?(): void;
  onError?(e: Error): void;
}

/** Streamed request (logs, stats, events, pull progress). A status of 400 or more ends the stream with onError. */
export function stream(method: string, path: string, opts: RequestOptions, h: StreamHandlers): StreamHandle {
  let closed = false;
  let inner: { close(): void } | undefined;
  versioned(path)
    .then((p) => {
      if (closed) return;
      let failed = false;
      let errBody = '';
      inner = getSdk().api.httpStream(HTTP_NAME, { method, path: p, ...opts }, {
        onStart: (status, headers) => {
          if (status >= 400) failed = true;
          else h.onStart?.(status, headers);
        },
        onData: (chunk) => {
          if (failed) errBody += new TextDecoder().decode(chunk);
          else h.onData(chunk);
        },
        onEnd: () => {
          if (failed) {
            let msg = errBody;
            try {
              msg = JSON.parse(errBody).message ?? errBody;
            } catch {
              /* plain text */
            }
            h.onError?.(new DockerError(500, msg || 'Docker refused the request'));
          } else h.onEnd?.();
        },
        onError: (e) => h.onError?.(e),
      });
    })
    .catch((e) => h.onError?.(e));
  return {
    close() {
      closed = true;
      inner?.close();
    },
  };
}

export const docker = {
  get: <T = unknown>(path: string, query?: Query) => json<T>('GET', path, { query }),
  post: <T = unknown>(path: string, query?: Query, body?: HttpRequest['body']) => json<T>('POST', path, { query, body }),
  put: <T = unknown>(path: string, query?: Query, body?: HttpRequest['body']) => json<T>('PUT', path, { query, body }),
  delete: <T = unknown>(path: string, query?: Query) => json<T>('DELETE', path, { query }),
  request,
  json,
  stream,
};

/* ---------- errors ---------- */

export type ErrorKind = 'unreachable' | 'forbidden' | 'admin' | 'engine' | 'unknown';

export interface ErrorInfo {
  kind: ErrorKind;
  message: string;
}

/** Sorts any thrown value into something a view can react to. */
export function classify(e: unknown): ErrorInfo {
  const err = e as Partial<PluginError> | undefined;
  const message = err?.message ?? String(e);
  if (e instanceof DockerError) return { kind: 'engine', message };
  switch (err?.code) {
    case 'needs_admin':
      return { kind: 'admin', message };
    case 'forbidden':
      return { kind: 'forbidden', message };
    case 'unavailable':
    case 'not_found':
      return { kind: 'unreachable', message };
    default:
      return { kind: /ECONNREFUSED|no such file|connect/i.test(message) ? 'unreachable' : 'unknown', message };
  }
}

/** The text to put in a toast for a failed action. */
export const errorText = (e: unknown): string => classify(e).message;
