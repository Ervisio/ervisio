import { apiUrl, FETCH_CREDENTIALS } from './base';
import { http } from './http';
import { requestUnlock } from './unlock';
import { ApiError } from './types';

/**
 * Large transfers for plugins (see server/internal/server/transfer.go). The daemon hands out a one-time URL, valid
 * for a minute, for a request it has already checked against the plugin's manifest; the bytes then flow between the
 * browser and the plugin's service without passing through the 16 MB RPC line.
 */
export interface TransferStart {
  /** Path of the one-time URL. */
  url: string;
  expires: number;
  /** Downloads: the sanitized file name, the size when the service sent it, the service's HTTP status. */
  filename?: string;
  size?: number;
  status?: number;
}

/** Asks for a transfer; opens the unlock dialog (once) when it needs administrator rights. */
export async function startTransfer(body: Record<string, unknown>, admin: boolean): Promise<TransferStart> {
  const go = () => http<TransferStart>('/api/plugins/transfer', { body });
  try {
    return await go();
  } catch (e) {
    if (admin && e instanceof ApiError && e.code === 'needs_admin') {
      await requestUnlock('plugins.transfer');
      return go();
    }
    throw e;
  }
}

/** Starts the browser's own download of a one-time URL: the file goes to disk, not through the page's memory. */
export function saveDownload(start: TransferStart): void {
  const a = document.createElement('a');
  a.href = apiUrl(start.url);
  a.download = start.filename ?? 'download';
  a.rel = 'noopener';
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  window.setTimeout(() => a.remove(), 1000);
}

export interface DownloadOutcome {
  ok: boolean;
  bytes: number;
  error?: string;
}

/**
 * Asks the daemon how a download ended (GET <url>/status, long poll). Resolves once with the outcome; if the daemon
 * cannot be asked (signed out, record gone) it resolves with ok:false and says so. Never rejects.
 */
export async function watchDownloadEnd(start: TransferStart, signal?: AbortSignal): Promise<DownloadOutcome> {
  // A started transfer lives 60 s unfetched; a download itself has no limit, so keep asking while it runs.
  for (;;) {
    try {
      const r = await http<{ done: boolean; ok?: boolean; bytes?: number; error?: string }>(`${start.url}/status?wait=20`, { signal });
      if (r.done) return { ok: !!r.ok, bytes: r.bytes ?? 0, ...(r.error ? { error: r.error } : {}) };
    } catch (e) {
      if (e instanceof ApiError && e.code === 'cancelled') return { ok: false, bytes: 0, error: 'The page was closed before the download ended.' };
      if (e instanceof ApiError && e.code !== 'network') return { ok: false, bytes: 0, error: 'The end of the download could not be read.' };
      await new Promise((res) => window.setTimeout(res, 2000)); // a network blip: ask again
    }
    if (signal?.aborted) return { ok: false, bytes: 0, error: 'The page was closed before the download ended.' };
  }
}

export interface UploadResult {
  status: number;
  headers: Record<string, string>;
  body: string;
  b64?: boolean;
  truncated?: boolean;
}

/**
 * Sends `file` as the body of a one-time upload URL. The browser reads the file from disk as it sends it;
 * onProgress gets the bytes sent so far. Aborting the signal cancels the transfer.
 * With `stream` (the transfer was started with stream: true) the response arrives as ndjson lines while the
 * file goes up, and is handed to `stream` piece by piece; the result then has an empty body.
 */
export interface UploadStream {
  /** The service answered. */
  onStart?(status: number, headers: Record<string, string>): void;
  /** A piece of the response body, as it arrives. */
  onData?(chunk: Uint8Array): void;
}

export function sendUpload(
  start: TransferStart,
  file: Blob,
  o: { onProgress?(loaded: number, total: number): void; signal?: AbortSignal; stream?: UploadStream },
): Promise<UploadResult> {
  return new Promise<UploadResult>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', apiUrl(start.url));
    xhr.withCredentials = FETCH_CREDENTIALS === 'include';
    xhr.setRequestHeader('X-Requested-With', 'ervisio');
    xhr.responseType = 'text';
    xhr.upload.onprogress = (ev) => o.onProgress?.(ev.loaded, ev.lengthComputable ? ev.total : file.size);
    xhr.onerror = () => reject(new ApiError('network', 'The upload was interrupted. Check the connection and try again.'));
    xhr.onabort = () => reject(new ApiError('cancelled', 'The upload was cancelled.'));
    // Streamed responses: ndjson lines {"start":{status,headers}}, {"data":"<base64>"}, {"done":true,"status"} or {"error":{}}.
    let seen = 0;
    let startInfo: { status: number; headers: Record<string, string> } | null = null;
    let finished: UploadResult | null = null;
    let streamError: ApiError | null = null;
    const readLines = (final: boolean) => {
      const text = xhr.responseText;
      let end = text.lastIndexOf('\n') + 1;
      if (final) end = text.length;
      const chunk = text.slice(seen, end);
      seen = end;
      for (const line of chunk.split('\n')) {
        if (!line.trim()) continue;
        let m: { start?: { status: number; headers: Record<string, string> }; data?: string; done?: boolean; status?: number; headers?: Record<string, string>; error?: { code?: string; message?: string } };
        try {
          m = JSON.parse(line);
        } catch {
          continue;
        }
        if (m.start) {
          startInfo = m.start;
          o.stream?.onStart?.(m.start.status, m.start.headers ?? {});
        } else if (typeof m.data === 'string') {
          const bin = atob(m.data);
          const bytes = new Uint8Array(bin.length);
          for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
          o.stream?.onData?.(bytes);
        } else if (m.done) {
          finished = { status: m.status ?? startInfo?.status ?? 0, headers: m.headers ?? startInfo?.headers ?? {}, body: '' };
        } else if (m.error) {
          streamError = new ApiError(m.error.code ?? 'internal', m.error.message ?? 'The upload failed.');
        }
      }
    };
    const streaming = () => !!o.stream && (xhr.getResponseHeader('Content-Type') ?? '').startsWith('application/x-ndjson');
    xhr.onprogress = () => {
      if (streaming()) readLines(false);
    };
    xhr.onload = () => {
      if (streaming()) {
        readLines(true);
        if (streamError) return reject(streamError);
        if (!finished) return reject(new ApiError('network', 'The response broke off before it ended.'));
        o.onProgress?.(file.size, file.size);
        return resolve(finished);
      }
      type Body = { result?: UploadResult; error?: { code?: string; message?: string; data?: unknown } };
      const parse = (): Body | null => {
        try {
          return JSON.parse(xhr.responseText) as Body;
        } catch {
          return null;
        }
      };
      const json = parse();
      if (xhr.status >= 200 && xhr.status < 300 && json?.result) {
        o.onProgress?.(file.size, file.size);
        resolve(json.result);
        return;
      }
      const code = json?.error?.code ?? (xhr.status === 401 ? 'unauthenticated' : xhr.status === 403 ? 'forbidden' : 'internal');
      reject(new ApiError(code, json?.error?.message ?? (xhr.statusText || 'The upload failed.'), json?.error?.data, xhr.status));
    };
    if (o.signal) {
      if (o.signal.aborted) return reject(new ApiError('cancelled', 'The upload was cancelled.'));
      o.signal.addEventListener('abort', () => xhr.abort(), { once: true });
    }
    // A File chosen in the sandboxed plugin frame belongs to that frame's process, which the browser refuses to read
    // from here (ERR_ACCESS_DENIED). A Blob made from it points at the same data without copying it into memory.
    xhr.send(new Blob([file]));
  });
}
