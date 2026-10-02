import { stream, type StreamHandle } from '../../api/engine';
import { registryAuthFor } from '../../api/registries';
import { JsonLines } from '../../api/streams';
import { splitRef } from './imageRef';

export interface PullLayer {
  id: string;
  status: string;
  current: number;
  total: number;
  /** 0 to 1 */
  pct: number;
  done: boolean;
}

export interface PullState {
  layers: PullLayer[];
  /** Last overall line, for example "Status: Downloaded newer image for nginx:latest". */
  message: string;
  finished: boolean;
  error?: string;
}

interface PullLine {
  id?: string;
  status?: string;
  error?: string;
  progressDetail?: { current?: number; total?: number };
}

const DONE = /^(Pull complete|Already exists|Download complete)$/;

/**
 * Pulls an image and reports progress after every change. Uses the stored login of the image's registry.
 * Returns a handle; close() cancels the download.
 */
export function pullImage(ref: string, onState: (s: PullState) => void): StreamHandle {
  const layers = new Map<string, PullLayer>();
  let message = '';
  let error: string | undefined;
  let finished = false;
  let handle: StreamHandle | undefined;
  let cancelled = false;
  const emit = () => onState({ layers: [...layers.values()], message, finished, error });

  const lines = new JsonLines<PullLine>((l) => {
    if (l.error) {
      error = l.error;
      emit();
      return;
    }
    if (l.id && l.status && !/^Pulling from/.test(l.status)) {
      const prev = layers.get(l.id) ?? { id: l.id, status: '', current: 0, total: 0, pct: 0, done: false };
      const d = l.progressDetail;
      const done = DONE.test(l.status);
      const total = d?.total ?? prev.total;
      const current = d?.current ?? prev.current;
      // Download fills the first half of the bar and extraction the second, so the bar never runs backwards.
      let pct = prev.pct;
      if (done) pct = 1;
      else if (total > 0 && /^Downloading/.test(l.status)) pct = Math.max(pct, (current / total) * 0.5);
      else if (total > 0 && /^Extracting/.test(l.status)) pct = Math.max(pct, 0.5 + (current / total) * 0.5);
      else if (/^Verifying/.test(l.status)) pct = Math.max(pct, 0.5);
      layers.set(l.id, { id: l.id, status: l.status, current, total, pct, done: l.status === 'Pull complete' || l.status === 'Already exists' });
    } else if (l.status) {
      message = l.status;
    }
    emit();
  });

  const { name, tag, digest } = splitRef(ref);
  void registryAuthFor(ref)
    .catch(() => undefined)
    .then((auth) => {
      if (cancelled) return;
      handle = stream(
        'POST',
        '/images/create',
        { query: { fromImage: name, ...(digest ? { tag: digest } : { tag }) }, headers: auth ? { 'X-Registry-Auth': auth } : undefined },
        {
          onData: (c) => lines.push(c),
          onEnd: () => {
            lines.end();
            finished = true;
            emit();
          },
          onError: (e) => {
            error = e.message;
            finished = true;
            emit();
          },
        },
      );
    });
  emit();
  return {
    close() {
      cancelled = true;
      handle?.close();
    },
  };
}
