import { mockCall } from './mock';
import { ApiError, type StreamHandle, type StreamHandlers } from './types';

/** Deterministic, cancellable project-search fixtures for the development daemon. */
export function mockStream<T>(
  method: string,
  input: unknown,
  handlers: StreamHandlers<T>,
): StreamHandle | null {
  if (method === 'files.copy') {
    let closed = false;
    queueMicrotask(() => {
      void mockCall(method, input, !!handlers.admin).then(
        (value) => {
          if (!closed) {
            handlers.onData?.(value as T);
            handlers.onEnd?.();
          }
        },
        (error) => {
          if (!closed) handlers.onError?.(error);
        },
      );
    });
    return {
      close() {
        closed = true;
      },
      send() {},
    };
  }
  if (method !== 'files.searchText' && method !== 'remote.searchText') return null;
  const params = input as {
    root: string;
    query: string;
    conn?: string;
    maxResults?: number;
    caseSensitive?: boolean;
    includeGenerated?: boolean;
  };
  let closed = false;
  const send = (value: unknown) => {
    if (!closed) handlers.onData?.(value as T);
  };
  const excluded = new Set([
    '.git',
    'node_modules',
    'vendor',
    '.venv',
    'venv',
    '__pycache__',
    '.cache',
    '.next',
    'dist',
    'build',
  ]);
  void (async () => {
    try {
      const query = params.caseSensitive ? params.query : params.query.toLowerCase();
      if (!query.trim() || query.length > 256 || /[\r\n\0]/.test(query))
        throw new ApiError('invalid', 'Enter a single line of text to search for.');
      const remote = method === 'remote.searchText';
      const queue = [params.root];
      let files = 0,
        skipped = 0,
        visited = 0,
        count = 0,
        truncated = false;
      const limit = Math.min(500, params.maxResults || 300);
      while (queue.length && !closed && !truncated) {
        const directory = queue.pop()!;
        const listing = await mockCall(
          remote ? 'remote.ls' : 'files.list',
          { path: directory, conn: params.conn, showHidden: true },
          !!handlers.admin,
        );
        for (const entry of listing.entries || []) {
          if (closed) return;
          if (++visited > 10000) {
            truncated = true;
            break;
          }
          const filePath =
            entry.path ||
            (directory === '/'
              ? '/' + entry.name
              : directory.replace(/[\\/]$/, '') + '/' + entry.name);
          if (entry.type === 'dir') {
            if (params.includeGenerated || !excluded.has(entry.name)) queue.push(filePath);
            else skipped++;
            continue;
          }
          if (entry.type !== 'file' || entry.size > 1 << 20) {
            skipped++;
            continue;
          }
          const value = await mockCall(
            remote ? 'remote.readText' : 'files.readText',
            { path: filePath, conn: params.conn, maxBytes: 1 << 20 },
            !!handlers.admin,
          );
          if (closed) return;
          if (value.truncated || value.content.includes('\0')) {
            skipped++;
            continue;
          }
          files++;
          const matches = [];
          for (const [index, text] of String(value.content).split('\n').entries()) {
            const at = (params.caseSensitive ? text : text.toLowerCase()).indexOf(query);
            if (at >= 0) {
              matches.push({
                path: filePath,
                line: index + 1,
                column: at + 1,
                text: text.slice(Math.max(0, at - 60), at + 180),
              });
              count++;
            }
            if (count >= limit) {
              truncated = true;
              break;
            }
          }
          send({ matches, files, skipped, count });
          if (truncated) break;
        }
      }
      send({ matches: [], files, skipped, count, done: true, truncated });
      if (!closed) handlers.onEnd?.();
    } catch (error) {
      if (!closed)
        handlers.onError?.(
          error instanceof ApiError ? error : new ApiError('internal', String(error)),
        );
    }
  })();
  return {
    close() {
      closed = true;
    },
    send() {},
  };
}
