import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";

const mockUsage = vi.fn();

vi.mock("@/api", () => ({
  files: { usage: (...args: any[]) => mockUsage(...args) },
}));

const mockRoute = { path: "/files/case/", query: {} };

vi.mock("vue-router", () => ({
  useRoute: () => mockRoute,
  useRouter: () => ({ push: vi.fn() }),
}));

import StorageCard from "../StorageCard.vue";
import { useUsageStore } from "@/stores/usage";

const GiB = 1024 ** 3;
const TOTAL = 1024 * GiB;
const STALE = 900 * GiB;
const FREED = 860 * GiB;

// Matches SETTLE_DELAY in StorageCard.
const SETTLE = 8000;

const i18n = createI18n({
  legacy: false,
  locale: "en",
  messages: {
    en: {
      files: {
        storage: "Storage",
        storageAvailable: "{available} available",
        usageViewAll: "View disk usage",
      },
    },
  },
});

const mountCard = () => mount(StorageCard, { global: { plugins: [i18n] } });

const used = (bytes: number) => ({ total: TOTAL, used: bytes });

describe("storage gauge refresh", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("re-reads the disk when something is deleted in the directory on screen", async () => {
    mockUsage.mockResolvedValue(used(STALE));
    const wrapper = mountCard();
    await flushPromises();

    expect(mockUsage).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("900 GiB");

    mockUsage.mockResolvedValue(used(FREED));
    useUsageStore().invalidate();
    await flushPromises();

    expect(mockUsage).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("860 GiB");
  });

  /*
   * The reason the immediate read is not enough on its own: ZFS applies the
   * free at the next transaction-group sync, measured 3.3-4.5s after the
   * unlink, so the read fired the moment the delete returns still sees the old
   * number.
   */
  it("reads again once the filesystem has committed the free", async () => {
    mockUsage.mockResolvedValue(used(STALE));
    const wrapper = mountCard();
    await flushPromises();

    useUsageStore().invalidate();
    await flushPromises();

    expect(mockUsage).toHaveBeenCalledTimes(2);
    expect(wrapper.text(), "statfs has not caught up yet").toContain("900 GiB");

    mockUsage.mockResolvedValue(used(FREED));
    await vi.advanceTimersByTimeAsync(SETTLE);
    await flushPromises();

    expect(mockUsage).toHaveBeenCalledTimes(3);
    expect(wrapper.text()).toContain("860 GiB");
  });

  /*
   * Deleting the file you are looking at lands you in its parent, which mounts
   * this card fresh — the change was signalled before the card existed, so the
   * follow-up has to be driven by how long ago it happened, not by the bump.
   */
  it("catches up when it mounts after the delete rather than during it", async () => {
    mockUsage.mockResolvedValue(used(STALE));
    useUsageStore().invalidate();

    const wrapper = mountCard();
    await flushPromises();
    expect(mockUsage).toHaveBeenCalledTimes(1);

    mockUsage.mockResolvedValue(used(FREED));
    await vi.advanceTimersByTimeAsync(SETTLE);
    await flushPromises();

    expect(mockUsage).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("860 GiB");
  });

  // Ordinary navigation must not leave a timer behind polling the disk.
  it("schedules nothing when no change was signalled", async () => {
    mockUsage.mockResolvedValue(used(STALE));
    mountCard();
    await flushPromises();

    await vi.advanceTimersByTimeAsync(60_000);
    await flushPromises();

    expect(mockUsage).toHaveBeenCalledTimes(1);
  });

  /*
   * Two reads can overlap now that a delete triggers one. The superseded read
   * rejects on its own abort, and treating that as a failure would drop 0 B
   * under the gauge for as long as the newer one takes to arrive.
   */
  it("does not flash an empty gauge when a read is superseded", async () => {
    const resolvers: Array<(value: unknown) => void> = [];

    mockUsage.mockImplementation(
      (_path: string, signal: AbortSignal) =>
        new Promise((resolve, reject) => {
          signal.addEventListener("abort", () => reject(new Error("canceled")));
          resolvers.push(resolve);
        })
    );

    const wrapper = mountCard();
    await flushPromises();

    resolvers[0](used(STALE));
    await flushPromises();
    expect(wrapper.text()).toContain("900 GiB");

    const store = useUsageStore();

    // The delete flow signals twice — once to drop the cache, once when the
    // removes land — so the second read routinely supersedes one still in
    // flight, which is the only way the first can lose its own abort race.
    store.invalidate();
    await flushPromises();
    expect(mockUsage).toHaveBeenCalledTimes(2);

    store.invalidate();
    await flushPromises();
    expect(mockUsage).toHaveBeenCalledTimes(3);

    expect(wrapper.text(), "the old figure should hold").toContain("900 GiB");
    expect(wrapper.text()).not.toContain("0 B");

    resolvers[2](used(FREED));
    await flushPromises();
    expect(wrapper.text()).toContain("860 GiB");
  });
});
