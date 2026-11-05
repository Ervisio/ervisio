interface Entry<T> {
  value?: T;
  updatedAt: number;
  generation: number;
  pending?: Promise<T>;
}

/** Memory-only cache of successful reads. A key must include the host and signed-in account. */
export class ScopedReadCache<T> {
  private entries = new Map<string, Entry<T>>();
  private now: () => number;

  constructor(now: () => number = Date.now) {
    this.now = now;
  }

  peek(key: string): T | undefined {
    return this.entries.get(key)?.value;
  }

  put(key: string, value: T): void {
    const entry = this.entry(key);
    entry.generation++;
    entry.value = value;
    entry.updatedAt = this.now();
    entry.pending = undefined;
  }

  /** Keep the last real value visible, but prevent an older request replacing newer data. */
  invalidate(key: string): void {
    const entry = this.entry(key);
    entry.generation++;
    entry.updatedAt = Number.NEGATIVE_INFINITY;
    entry.pending = undefined;
  }

  load(key: string, read: () => Promise<T>, ttl: number, force = false): Promise<T> {
    const entry = this.entry(key);
    if (entry.pending) return entry.pending;
    if (!force && entry.value !== undefined && this.now() - entry.updatedAt < ttl) {
      return Promise.resolve(entry.value);
    }
    const generation = entry.generation;
    // Start now, while the caller's authenticated account is still current.
    const request = read().then(
      (value) => {
        if (entry.generation === generation) {
          entry.value = value;
          entry.updatedAt = this.now();
        }
        return value;
      },
      (error: unknown) => {
        // A failed manual refresh must remain retryable even inside the old TTL.
        if (entry.generation === generation) entry.updatedAt = Number.NEGATIVE_INFINITY;
        throw error;
      },
    );
    const pending = request.finally(() => {
      if (entry.pending === pending) entry.pending = undefined;
    });
    entry.pending = pending;
    return pending;
  }

  private entry(key: string): Entry<T> {
    let entry = this.entries.get(key);
    if (!entry) {
      entry = { updatedAt: Number.NEGATIVE_INFINITY, generation: 0 };
      this.entries.set(key, entry);
      // Avoid retaining catalogs indefinitely when many accounts use one browser.
      if (this.entries.size > 8) this.entries.delete(this.entries.keys().next().value!);
    }
    return entry;
  }
}
