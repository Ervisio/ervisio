/** Pull an image with progress: POST /images/create streamed as JSON lines. */
import { docker } from '../../api/engine';
import { JsonLines } from '../../api/streams';
import { registryAuthFor } from '../../api/registries';
import { imagePath } from './model';

export interface PullState {
  /** The Engine's latest status line, for example "Downloading". */
  status: string;
  /** 0 to 100, or undefined before the sizes are known. */
  percent?: number;
  layers: number;
  doneLayers: number;
}

/** Splits "repo/name:tag@sha256:..." into what /images/create wants. */
export function parseRef(ref: string): { from: string; tag?: string } {
  const r = ref.trim();
  if (r.includes('@')) return { from: r };
  const slash = r.lastIndexOf('/');
  const colon = r.lastIndexOf(':');
  if (colon > slash) return { from: r.slice(0, colon), tag: r.slice(colon + 1) };
  return { from: r, tag: 'latest' };
}

export interface PullHandle {
  done: Promise<void>;
  cancel(): void;
}

export function pullImage(ref: string, onState: (s: PullState) => void): PullHandle {
  let handle: { close(): void } | undefined;
  let cancelled = false;
  const done = (async () => {
    const { from, tag } = parseRef(ref);
    let auth: string | undefined;
    try {
      auth = await registryAuthFor(ref.trim());
    } catch {
      auth = undefined;
    }
    await new Promise<void>((resolve, reject) => {
      const layers = new Map<string, { cur: number; tot: number; done: boolean }>();
      let failure = '';
      const parser = new JsonLines<{ id?: string; status?: string; error?: string; progressDetail?: { current?: number; total?: number } }>((v) => {
        if (v.error) {
          failure = v.error;
          return;
        }
        if (v.id && v.status) {
          const l = layers.get(v.id) ?? { cur: 0, tot: 0, done: false };
          const pd = v.progressDetail;
          if (pd?.total) {
            l.cur = pd.current ?? 0;
            l.tot = pd.total;
          }
          if (/Pull complete|Already exists/.test(v.status)) {
            l.done = true;
            if (l.tot) l.cur = l.tot;
          }
          layers.set(v.id, l);
        }
        let cur = 0;
        let tot = 0;
        let doneN = 0;
        layers.forEach((l) => {
          cur += l.cur;
          tot += l.tot;
          if (l.done) doneN++;
        });
        onState({ status: v.status ? `${v.status}${v.id ? ` ${v.id}` : ''}` : '', percent: tot ? Math.min(99, Math.round((cur / tot) * 100)) : undefined, layers: layers.size, doneLayers: doneN });
      });
      handle = docker.stream(
        'POST',
        '/images/create',
        { query: tag ? { fromImage: from, tag } : { fromImage: from }, headers: auth ? { 'X-Registry-Auth': auth } : undefined },
        {
          onData: (c) => parser.push(c),
          onEnd: () => {
            parser.end();
            if (failure) reject(new Error(failure));
            else if (cancelled) reject(new Error('cancelled'));
            else resolve();
          },
          onError: (e) => reject(e),
        },
      );
    });
  })();
  return {
    done,
    cancel() {
      cancelled = true;
      handle?.close();
    },
  };
}

export interface LocalImage {
  Id: string;
  Size: number;
  Created: string;
  Config?: { Env?: string[] | null; Cmd?: string[] | null; Entrypoint?: string[] | null; Labels?: Record<string, string> | null; WorkingDir?: string; Healthcheck?: unknown };
}

/** The image when it is on this host, undefined when not, throws on other errors. */
export async function localImage(ref: string): Promise<LocalImage | undefined> {
  const r = await docker.request('GET', `/images/${imagePath(ref.trim())}/json`);
  if (r.status === 404) return undefined;
  if (r.status >= 400) throw new Error(r.json()?.message ?? `Docker answered ${r.status}`);
  return r.json() as LocalImage;
}
