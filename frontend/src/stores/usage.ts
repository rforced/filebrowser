import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import { files as api } from "@/api";

/*
 * Directory sizes, shared by every surface that shows them.
 *
 * A size costs a full recursive stat of the tree, which on a filesystem full
 * of solver output is the expensive part of all of this. Four places want the
 * same numbers — the usage view, the storage card's top consumers, the
 * listing's per-folder buttons and the info prompt — so they resolve through
 * one store rather than each walking the tree again. Filling the usage view
 * for a directory answers every folder button in that listing for free, and
 * vice versa.
 *
 * Entries are keyed by the resource path (not the /files URL).
 */

const CACHE_TTL = 60 * 1000;

// Walks are stat storms against network-backed storage. Letting a listing fire
// one per folder at once would bury the disk and starve the request the user
// is actually waiting on.
const MAX_CONCURRENT = 3;

export interface UsageRecord {
  size: number;
  logicalSize: number;
  numFiles: number;
  numDirs: number;
  at: number;
}

type Waiter = () => void;

export const useUsageStore = defineStore("usage", () => {
  const sizes = reactive(new Map<string, UsageRecord>());
  const pending = reactive(new Set<string>());
  const failed = reactive(new Set<string>());

  // Kept out of the reactive state: these are plumbing, not something a
  // template should re-render on.
  const inflight = new Map<string, Promise<DirSizeInfo | null>>();
  const controllers = new Map<string, AbortController>();
  const queue: Waiter[] = [];
  const running = ref(0);

  const fresh = (path: string): UsageRecord | undefined => {
    const hit = sizes.get(path);
    if (hit && Date.now() - hit.at < CACHE_TTL) return hit;
    return undefined;
  };

  /*
   * Only record a response that actually carries sizes. filesize() throws on a
   * non-number, and a throw inside a listing row's render unmounts the row —
   * so an unexpected response shape would make a folder vanish from the
   * listing rather than merely display wrong. Failing loudly into `failed` is
   * the honest outcome.
   */
  const record = (path: string, info: DirSizeInfo): boolean => {
    if (!Number.isFinite(info?.size) || !Number.isFinite(info?.logicalSize)) {
      failed.add(path);
      return false;
    }

    sizes.set(path, {
      size: info.size,
      logicalSize: info.logicalSize,
      numFiles: info.numFiles ?? 0,
      numDirs: info.numDirs ?? 0,
      at: Date.now(),
    });
    failed.delete(path);
    return true;
  };

  const pump = () => {
    while (running.value < MAX_CONCURRENT && queue.length > 0) {
      running.value++;
      queue.shift()!();
    }
  };

  const slot = (): Promise<void> =>
    new Promise((resolve) => {
      queue.push(resolve);
      pump();
    });

  const release = () => {
    running.value--;
    pump();
  };

  /*
   * Resolve one directory's size, coalescing concurrent callers and queueing
   * behind the concurrency cap. Returns null if the walk was cancelled or
   * failed; callers render that as "unknown" rather than as zero, which would
   * be a lie about a directory that may be enormous.
   */
  const measure = async (path: string): Promise<DirSizeInfo | null> => {
    const hit = fresh(path);
    if (hit) return hit;

    const existing = inflight.get(path);
    if (existing) return existing;

    const run = (async () => {
      await slot();

      const controller = new AbortController();
      controllers.set(path, controller);
      pending.add(path);

      try {
        const info = await api.dirSize(path, controller.signal);
        return record(path, info) ? info : null;
      } catch {
        // An abort is a deliberate cancellation, not a failure worth marking:
        // the row goes back to offering the button.
        if (!controller.signal.aborted) failed.add(path);
        return null;
      } finally {
        pending.delete(path);
        controllers.delete(path);
        inflight.delete(path);
        release();
      }
    })();

    inflight.set(path, run);
    return run;
  };

  /*
   * Fetch a whole directory's breakdown in one walk and seed every child into
   * the cache, so the listing's folder rows are answered without a request
   * each.
   */
  const breakdown = async (
    path: string,
    opts: { kinds?: boolean; signal?: AbortSignal } = {}
  ): Promise<UsageBreakdown> => {
    const result = await api.usageBreakdown(path, opts);

    const base = path.endsWith("/") ? path : `${path}/`;
    for (const child of result.children) {
      if (!child.isDir) continue;
      record(`${base}${child.name}`, child);
    }
    record(path, result);

    return result;
  };

  const cancel = (path: string) => {
    controllers.get(path)?.abort();
  };

  const cancelAll = () => {
    for (const controller of controllers.values()) controller.abort();
    queue.length = 0;
    running.value = 0;
  };

  const revision = ref(0);
  const changedAt = ref(0);

  const invalidate = (path?: string) => {
    revision.value++;
    changedAt.value = Date.now();

    if (path === undefined) {
      sizes.clear();
      failed.clear();
      return;
    }
    sizes.delete(path);
    failed.delete(path);
  };

  return {
    sizes,
    pending,
    failed,
    revision,
    changedAt,
    fresh,
    measure,
    breakdown,
    cancel,
    cancelAll,
    invalidate,
  };
});
