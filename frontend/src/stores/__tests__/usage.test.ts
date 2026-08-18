import { describe, expect, it, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

const api = vi.hoisted(() => ({
  dirSize: vi.fn(),
  usageBreakdown: vi.fn(),
}));

vi.mock("@/api", () => ({ files: api }));

import { useUsageStore } from "../usage";

const info = (size: number, logicalSize = size) => ({
  size,
  logicalSize,
  numFiles: 1,
  numDirs: 0,
});

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("usage store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });

  it("caches a measured directory instead of walking it twice", async () => {
    api.dirSize.mockResolvedValue(info(4096, 8192));
    const store = useUsageStore();

    await store.measure("/cases/a");
    await store.measure("/cases/a");

    expect(api.dirSize).toHaveBeenCalledOnce();
    expect(store.sizes.get("/cases/a")?.size).toBe(4096);
    expect(store.sizes.get("/cases/a")?.logicalSize).toBe(8192);
  });

  // Two rows clicked in quick succession are one walk, not two.
  it("coalesces concurrent callers onto one request", async () => {
    let release: (v: unknown) => void = () => {};
    api.dirSize.mockReturnValue(
      new Promise((resolve) => {
        release = resolve;
      })
    );
    const store = useUsageStore();

    const first = store.measure("/cases/a");
    const second = store.measure("/cases/a");

    // A walk waits for a concurrency slot before it starts, so it is marked
    // pending a tick later rather than synchronously.
    await tick();
    expect(store.pending.has("/cases/a")).toBe(true);

    release(info(2048));
    await Promise.all([first, second]);

    expect(api.dirSize).toHaveBeenCalledOnce();
    expect(store.pending.has("/cases/a")).toBe(false);
  });

  /*
   * A failed walk must not be cached as a size. Recording zero would claim a
   * directory is empty when it may be the biggest thing on the disk.
   */
  it("marks a failure without inventing a size", async () => {
    api.dirSize.mockRejectedValue(new Error("boom"));
    const store = useUsageStore();

    expect(await store.measure("/cases/a")).toBe(null);
    expect(store.sizes.has("/cases/a")).toBe(false);
    expect(store.failed.has("/cases/a")).toBe(true);
  });

  it("never runs more than a few walks at once", async () => {
    let live = 0;
    let peak = 0;
    const releases: Array<() => void> = [];

    api.dirSize.mockImplementation(
      () =>
        new Promise((resolve) => {
          live++;
          peak = Math.max(peak, live);
          releases.push(() => {
            live--;
            resolve(info(1024));
          });
        })
    );

    const store = useUsageStore();
    const all = Promise.all(
      Array.from({ length: 10 }, (_, i) => store.measure(`/cases/${i}`))
    );

    // Release in waves, ticking first so the queued walks have actually
    // started. Each wave frees slots the queue then has to refill, which is
    // where a broken cap would show up as a peak above the limit.
    for (let wave = 0; wave < 10; wave++) {
      await tick();
      releases.splice(0).forEach((release) => release());
    }
    await all;

    expect(api.dirSize).toHaveBeenCalledTimes(10);
    expect(peak).toBeLessThanOrEqual(3);
  });

  /*
   * The point of the shared store: one breakdown request answers every folder
   * row in that listing, so the per-row buttons never have to ask again.
   */
  it("seeds child directories from a breakdown", async () => {
    api.usageBreakdown.mockResolvedValue({
      size: 9000,
      logicalSize: 12000,
      numFiles: 3,
      numDirs: 2,
      children: [
        { name: "big", isDir: true, ...info(8000, 10000) },
        { name: "small", isDir: true, ...info(1000, 2000) },
        { name: "loose.dat", isDir: false, ...info(500, 500) },
      ],
    });

    const store = useUsageStore();
    await store.breakdown("/cases");

    expect(store.sizes.get("/cases/big")?.size).toBe(8000);
    expect(store.sizes.get("/cases/small")?.size).toBe(1000);
    // Files are not measurable units here — nothing should offer to walk one.
    expect(store.sizes.has("/cases/loose.dat")).toBe(false);

    await store.measure("/cases/big");
    expect(api.dirSize).not.toHaveBeenCalled();
  });

  /*
   * A response without sizes must not be cached. filesize() throws on a
   * non-number and a throw during a listing row's render unmounts the row, so
   * recording one would make the folder vanish from the listing instead of
   * merely showing something wrong.
   */
  it("refuses a response that carries no sizes", async () => {
    api.dirSize.mockResolvedValue({ items: [], numFiles: 3, numDirs: 1 });
    const store = useUsageStore();

    expect(await store.measure("/cases")).toBe(null);
    expect(store.sizes.has("/cases")).toBe(false);
    expect(store.failed.has("/cases")).toBe(true);
  });

  it("forgets what a caller says has changed", async () => {
    api.dirSize.mockResolvedValue(info(4096));
    const store = useUsageStore();

    await store.measure("/cases/a");
    store.invalidate("/cases/a");
    await store.measure("/cases/a");

    expect(api.dirSize).toHaveBeenCalledTimes(2);
  });
});
