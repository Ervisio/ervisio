import { ApiError, requestUnlock, stream, uploadUrl } from '../../api';
import { emitChanged, fcall, isUnlocked } from './fapi';
import { useStore } from '../../lib/useStore';
import { basename, dirname, nextId } from './util';

export type TransferKind = 'upload' | 'copy' | 'move';
export type TransferStatus = 'queued' | 'running' | 'done' | 'error' | 'cancelled';

export interface Transfer {
  id: string;
  kind: TransferKind;
  name: string;
  /** Where it goes (folder path). */
  dest: string;
  total: number;
  done: number;
  status: TransferStatus;
  error?: string;
  errorCode?: string;
  /** bytes per second, smoothed */
  speed?: number;
  items?: number;
}

interface Job extends Transfer {
  run(): void;
  cancel(): void;
  /** Repeat the transfer replacing an existing target (after a conflict). */
  replace?: () => void;
  retry?: () => void;
}

let jobs: Job[] = [];
let snapshot: Transfer[] = [];
const subs = new Set<() => void>();
const MAX_UPLOADS = 2;


function emit() {
  snapshot = jobs.map(({ run: _r, cancel: _c, replace: _p, retry: _t, ...t }) => t);
  subs.forEach((f) => f());
}
function patch(id: string, p: Partial<Transfer>) {
  const j = jobs.find((x) => x.id === id);
  if (!j) return;
  Object.assign(j, p);
  emit();
}
function pump() {
  const running = jobs.filter((j) => j.kind === 'upload' && j.status === 'running').length;
  let free = MAX_UPLOADS - running;
  for (const j of jobs) {
    if (free <= 0) break;
    if (j.kind === 'upload' && j.status === 'queued') {
      j.status = 'running';
      free--;
      j.run();
    }
  }
  emit();
}

export function useTransfers(): Transfer[] {
  return useStore(subs, () => snapshot);
}

export const transferActions = {
  cancel(id: string) {
    const j = jobs.find((x) => x.id === id);
    if (!j) return;
    if (j.status === 'queued' || j.status === 'running') {
      const wasRunning = j.status === 'running';
      j.status = 'cancelled';
      if (wasRunning) j.cancel();
      emit();
      pump();
    }
  },
  replace(id: string) {
    jobs.find((x) => x.id === id)?.replace?.();
  },
  retry(id: string) {
    jobs.find((x) => x.id === id)?.retry?.();
  },
  clearFinished() {
    jobs = jobs.filter((j) => j.status === 'queued' || j.status === 'running');
    emit();
  },
  remove(id: string) {
    jobs = jobs.filter((j) => j.id !== id || j.status === 'running' || j.status === 'queued');
    emit();
  },
};

/* ---------------- uploads ---------------- */

interface UploadOpts {
  file: File;
  /** Folder to put the file into. */
  dest: string;
  /** Relative path inside dest (for dropped folders), e.g. "photos/a.jpg". */
  rel?: string;
  admin: boolean;
  onElevate?: () => void;
}

/** Picks the error code for a failed upload response. */
function uploadErrorCode(status: number, body: any): string {
  return body?.error?.code ?? (status === 409 ? 'conflict' : status === 403 ? 'needs_admin' : 'internal');
}

function parseJson(text: string): any {
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

export function enqueueUpload(o: UploadOpts) {
  const rel = o.rel ?? o.file.name;
  const target = o.dest === '/' ? '/' + rel : o.dest + '/' + rel;
  const id = nextId();
  let xhr: XMLHttpRequest | null = null;
  let admin = o.admin;
  let overwrite = false;
  let elevated = false;
  let last = { t: 0, b: 0 };

  const fail = (errorCode: string, error: string) => {
    patch(id, { status: 'error', errorCode, error });
    pump();
  };

  const onProgress = (ev: ProgressEvent) => {
    const now = performance.now();
    const dt = (now - last.t) / 1000;
    let speed: number | undefined;
    if (dt > 0.5) {
      speed = (ev.loaded - last.b) / dt;
      last = { t: now, b: ev.loaded };
    }
    patch(id, { done: ev.loaded, ...(speed !== undefined ? { speed } : {}) });
  };

  const elevateAndRestart = (code: string, body: any) => {
    elevated = true;
    void (isUnlocked() ? Promise.resolve() : requestUnlock('upload')).then(
      () => {
        admin = true;
        o.onElevate?.();
        patch(id, { status: 'running', done: 0 });
        start();
      },
      () => fail(code, body?.error?.message ?? 'Administrator rights are needed to write here.'),
    );
  };

  const onLoad = (req: XMLHttpRequest) => {
    if (req.status >= 200 && req.status < 300) {
      patch(id, { status: 'done', done: o.file.size, speed: undefined });
      pump();
      emitChanged(dirname(target));
      return;
    }
    const body = parseJson(req.responseText);
    const code = uploadErrorCode(req.status, body);
    if (code === 'needs_admin' && !admin && !elevated) {
      elevateAndRestart(code, body);
      return;
    }
    fail(code, body?.error?.message ?? `Upload failed (${req.status}).`);
  };

  const start = () => {
    const req = new XMLHttpRequest();
    xhr = req;
    req.open('POST', uploadUrl(target, admin, overwrite));
    req.setRequestHeader('X-Requested-With', 'ervisio');
    req.withCredentials = true;
    last = { t: performance.now(), b: 0 };
    req.upload.onprogress = onProgress;
    req.onload = () => onLoad(req);
    req.onerror = () => fail('network', 'The connection was lost.');
    req.send(o.file);
  };

  const requeue = () => {
    patch(id, { status: 'queued', done: 0, error: undefined, errorCode: undefined });
    pump();
  };

  const job: Job = {
    id,
    kind: 'upload',
    name: rel,
    dest: o.dest,
    total: o.file.size,
    done: 0,
    status: 'queued',
    run: start,
    cancel: () => xhr?.abort(),
    replace: () => {
      overwrite = true;
      requeue();
    },
    retry: requeue,
  };
  jobs.push(job);
  emit();
  pump();
}

/** Makes a folder before uploading into it (used for dropped folders). */
export async function ensureDir(path: string, admin: boolean, onElevate?: () => void) {
  try {
    await fcall('files.mkdir', { path, parents: true }, { admin, onElevate });
  } catch (e) {
    if (!(e instanceof ApiError && e.code === 'conflict')) throw e;
  }
}

/* ---------------- copy / move (stream) ---------------- */

export interface CopyOpts {
  from: string[];
  to: string;
  move: boolean;
  admin: boolean;
  onElevate?: () => void;
  onDone?: () => void;
}

type CopyMsg = { type?: string; total?: number; items?: number; done?: number; current?: string };

export function enqueueCopy(o: CopyOpts) {
  const id = nextId();
  const label = o.from.length === 1 ? basename(o.from[0]) : `${o.from.length} items`;
  let handle: { close(): void } | null = null;
  let admin = o.admin;
  let elevated = false;

  const onData = (m: CopyMsg) => {
    if (m.type === 'start') patch(id, { total: m.total, items: m.items });
    else if (m.type === 'progress') patch(id, { done: m.done, total: m.total, name: o.from.length === 1 ? label : `${label} · ${m.current}` });
    else if (m.done) patch(id, { done: -1 });
  };

  const onEnd = () => {
    const j = jobs.find((x) => x.id === id);
    if (j && j.status === 'running') {
      patch(id, { status: 'done', done: j.total });
      emitChanged();
      o.onDone?.();
    }
  };

  const onError = (e: { code: string; message: string }) => {
    const j = jobs.find((x) => x.id === id);
    if (!j || j.status === 'cancelled') return;
    if (e.code === 'needs_admin' && !admin && !elevated && isUnlocked()) {
      // the stream layer already asked for the password; run the copy on the root bridge now
      elevated = true;
      admin = true;
      o.onElevate?.();
      begin();
      return;
    }
    patch(id, { status: 'error', errorCode: e.code, error: e.message });
    emitChanged();
  };

  const begin = () => {
    patch(id, { status: 'running', done: 0, error: undefined });
    handle = stream<CopyMsg>('files.copy', { from: o.from, to: o.to, move: o.move }, { admin, onData, onEnd, onError });
  };

  const job: Job = {
    id,
    kind: o.move ? 'move' : 'copy',
    name: label,
    dest: o.to,
    total: 0,
    done: 0,
    status: 'running',
    run: begin,
    cancel: () => {
      handle?.close();
      emitChanged();
    },
    retry: begin,
  };
  jobs.push(job);
  emit();
  begin();
}
