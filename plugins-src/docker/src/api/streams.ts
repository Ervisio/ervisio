/** Parsers for the Engine's streamed bodies. */

/** Splits a byte stream into JSON values, one per line (stats, events, pull progress, build output). */
export class JsonLines<T = any> {
  private buf = '';
  private dec = new TextDecoder();
  constructor(
    private onValue: (v: T) => void,
    private onBad?: (line: string) => void,
  ) {}
  push(chunk: Uint8Array): void {
    this.buf += this.dec.decode(chunk, { stream: true });
    let i: number;
    while ((i = this.buf.indexOf('\n')) >= 0) {
      const line = this.buf.slice(0, i).trim();
      this.buf = this.buf.slice(i + 1);
      if (!line) continue;
      try {
        this.onValue(JSON.parse(line) as T);
      } catch {
        this.onBad?.(line);
      }
    }
  }
  /** Call at the end of the stream to flush a last line without newline. */
  end(): void {
    const line = (this.buf + this.dec.decode()).trim();
    this.buf = '';
    if (!line) return;
    try {
      this.onValue(JSON.parse(line) as T);
    } catch {
      this.onBad?.(line);
    }
  }
}

export type LogStream = 'stdin' | 'stdout' | 'stderr';

/**
 * Demultiplexes a container log or exec stream. Without a tty the Engine sends frames: an 8-byte header
 * [stream, 0, 0, 0, size (uint32 big endian)] followed by `size` bytes. With a tty the bytes are raw (stdout only).
 */
export class LogDemuxer {
  private pending = new Uint8Array(0);
  constructor(
    private tty: boolean,
    private onFrame: (stream: LogStream, data: Uint8Array) => void,
  ) {}
  push(chunk: Uint8Array): void {
    if (this.tty) {
      this.onFrame('stdout', chunk);
      return;
    }
    let buf: Uint8Array;
    if (this.pending.length) {
      buf = new Uint8Array(this.pending.length + chunk.length);
      buf.set(this.pending);
      buf.set(chunk, this.pending.length);
    } else buf = chunk;
    let off = 0;
    while (buf.length - off >= 8) {
      const size = ((buf[off + 4] << 24) | (buf[off + 5] << 16) | (buf[off + 6] << 8) | buf[off + 7]) >>> 0;
      if (buf.length - off - 8 < size) break;
      const kind = buf[off] === 2 ? 'stderr' : buf[off] === 0 ? 'stdin' : 'stdout';
      this.onFrame(kind, buf.subarray(off + 8, off + 8 + size));
      off += 8 + size;
    }
    this.pending = off < buf.length ? buf.slice(off) : new Uint8Array(0);
  }
}

export interface LogLine {
  stream: LogStream;
  /** RFC 3339 timestamp when the request used timestamps=1, else ''. */
  ts: string;
  text: string;
}

/**
 * Container logs as lines: demultiplexes (unless tty), splits into lines per stream and optionally cuts the
 * timestamp the Engine prepends with `timestamps=1`.
 */
export class LogLines {
  private demux: LogDemuxer;
  private dec: Record<string, TextDecoder> = {};
  private rest: Record<string, string> = {};
  constructor(
    tty: boolean,
    private timestamps: boolean,
    private onLine: (l: LogLine) => void,
  ) {
    this.demux = new LogDemuxer(tty, (s, data) => this.frame(s, data));
  }
  push(chunk: Uint8Array): void {
    this.demux.push(chunk);
  }
  /** Emits lines still waiting for a newline. */
  flush(): void {
    for (const s of Object.keys(this.rest)) {
      if (this.rest[s]) this.emit(s as LogStream, this.rest[s]);
      this.rest[s] = '';
    }
  }
  private frame(s: LogStream, data: Uint8Array): void {
    const dec = (this.dec[s] ??= new TextDecoder());
    const text = (this.rest[s] ?? '') + dec.decode(data, { stream: true });
    const parts = text.split('\n');
    this.rest[s] = parts.pop() ?? '';
    for (const p of parts) this.emit(s, p.endsWith('\r') ? p.slice(0, -1) : p);
  }
  private emit(stream: LogStream, line: string): void {
    if (this.timestamps) {
      const sp = line.indexOf(' ');
      if (sp > 0 && /^\d{4}-\d\d-\d\dT/.test(line)) {
        this.onLine({ stream, ts: line.slice(0, sp), text: line.slice(sp + 1) });
        return;
      }
    }
    this.onLine({ stream, ts: '', text: line });
  }
}
