// One cache shape serves every per-frame payload playback touches (surfaces,
// parcel clouds): a byte-bounded LRU of shared promises, so a prefetch and the
// view asking for the same frame join one in-flight request — scrubbing back,
// prefetching ahead, and the route sync on pause all cost one fetch each.

export interface FrameCache<T> {
  fetch(key: string, load: (signal: AbortSignal) => Promise<T>): Promise<T>;
  clear(): void;
}

interface Entry<T> {
  promise: Promise<T>;
  bytes: number;
  controller: AbortController;
}

export function createFrameCache<T>(
  maxEntries: number,
  maxBytes: number,
  sizeOf: (value: T) => number
): FrameCache<T> {
  const entries = new Map<string, Entry<T>>();
  let totalBytes = 0;

  // Oldest-first until inside budget; the newest entry always survives, even
  // alone over the byte budget — evicting what was just asked for would turn
  // a large case into a refetch loop.
  const evict = () => {
    for (const [key, entry] of entries) {
      if (entries.size <= 1) return;
      if (entries.size <= maxEntries && totalBytes <= maxBytes) return;
      entries.delete(key);
      totalBytes -= entry.bytes;
    }
  };

  const fetch = (key: string, load: (signal: AbortSignal) => Promise<T>) => {
    const hit = entries.get(key);
    if (hit) {
      entries.delete(key);
      entries.set(key, hit);
      return hit.promise;
    }

    const controller = new AbortController();
    const entry = { bytes: 0, controller } as Entry<T>;
    entry.promise = load(controller.signal).then(
      (data) => {
        entry.bytes = sizeOf(data);
        // An entry evicted while it was still in flight is no longer the
        // cache's to account for. Charging it here would strand those bytes,
        // and the budget would ratchet down until nothing stayed cached.
        if (entries.get(key) === entry) {
          totalBytes += entry.bytes;
          evict();
        }
        return data;
      },
      (err) => {
        // A failed fetch must not poison the cache, or retrying would replay
        // the failure from memory.
        if (entries.get(key) === entry) {
          entries.delete(key);
        }
        throw err;
      }
    );
    entries.set(key, entry);
    evict();
    return entry.promise;
  };

  return {
    fetch,
    // Aborting is the point, not the clearing: a payload still in flight
    // costs the server a full pass over a post file, and the viewer that
    // asked for it is gone. Dropping the entries alone would leave that work
    // running to completion.
    clear() {
      for (const entry of entries.values()) {
        entry.controller.abort();
      }
      entries.clear();
      totalBytes = 0;
    },
  };
}
