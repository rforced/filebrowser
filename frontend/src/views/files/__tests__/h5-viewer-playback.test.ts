import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";

vi.mock("@/utils/constants", () => ({
  baseURL: "/test",
  origin: "http://localhost",
  name: "Test",
  staticURL: "/static",
  disableExternal: false,
  disableUsedPercentage: false,
  recaptcha: "",
  recaptchaKey: "",
  version: "0.0.0",
  authMethod: "password",
  logoutPage: "",
  theme: "light",
  enableThumbs: false,
  resizePreview: false,
  tusSettings: { retryCount: 5, chunkSize: 10485760 },
  tusEndpoint: "/api/tus",
  logoURL: "/static/img/logo.svg",
  hideLoginButton: false,
  domain: "",
  teamId: "",
  filesystemId: "",
}));

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" } } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

const mockFetch = vi.fn();

vi.mock("@/api", () => ({
  files: {
    getDownloadURL: () => "/download",
    fetch: (...args: any[]) => mockFetch(...args),
  },
}));

const mockSummary = vi.fn();
const mockSurface = vi.fn();

vi.mock("@/api/h5", () => ({
  summary: (...args: any[]) => mockSummary(...args),
  surface: (...args: any[]) => mockSurface(...args),
  stats: vi.fn(),
  subsetURL: () => "/subset",
}));

vi.mock("vue-router", () => ({
  useRoute: () => ({
    path: "/files/case/post000001_+1.00000e+00.h5",
    query: {},
  }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

import { useFileStore } from "@/stores/file";
import { clearSurfaceCache } from "@/utils/surfaceCache";
import H5Viewer from "../H5Viewer.vue";

// A post file with a surface stream, which is what puts the surface tab and
// its playback controls on screen.
const postSummary = {
  name: "post000001_+1.00000e+00.h5",
  size: 1024,
  kind: "post" as const,
  // The surface tab is offered only for a file that names boundaries and has a
  // stream carrying faces.
  boundaries: [{ id: 1, name: "PISTON", elements: 4 }],
  streams: [
    {
      name: "STREAM_00",
      cells: 4,
      faces: 8,
      vertices: 8,
      variables: [
        {
          name: "TEMPERATURE",
          path: "STREAM_00/CELL_CENTER_DATA/TEMPERATURE",
          type: "float32",
          dims: [4],
          bytes: 16,
        },
      ],
    },
  ],
};

const frameListing = {
  items: [
    { name: "post000001_+1.00000e+00.h5", size: 1024, isDir: false },
    { name: "post000002_+2.00000e+00.h5", size: 1024, isDir: false },
    { name: "post000003_+3.00000e+00.h5", size: 1024, isDir: false },
  ],
};

function mountViewer() {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: {
      en: {
        h5View: {
          surface: "Surface",
          parcels: "Parcels",
          variables: "Variables",
          playbackCapped:
            "Playback stopped after {minutes} minutes. Press play to carry on.",
          surfacePlaybackOff:
            "Too large to play back: {faces} faces. Parcels still play.",
          resolution: "Detail",
          resLow: "Low",
          resMedium: "Medium",
          resHigh: "High",
          resUltra: "Ultra",
        },
        buttons: { play: "Play", pause: "Pause" },
      },
    },
  });

  return mount(H5Viewer, {
    global: {
      plugins: [i18n],
      directives: { tooltip: {} },
      stubs: {
        ParcelCloud: true,
        BoundarySurface: true,
        RouterLink: true,
      },
    },
  });
}

// Playback is paced by the surface reporting itself loaded, so the stub has to
// stand in for that signal to make the loop turn at all.
async function settleFrame(wrapper: any) {
  const surface = wrapper.findComponent({ name: "BoundarySurface" });
  if (surface.exists()) {
    surface.vm.$emit("loaded", [0, 1]);
  }
  await flushPromises();
}

// One turn of the loop: let the pending advance fire, then stand in for the
// surface that lands. A jump only fires timers already due at the time they
// were scheduled for, so crossing the cap takes a further turn to be noticed.
async function turn(wrapper: any, ms: number) {
  vi.advanceTimersByTime(ms);
  await flushPromises();
  await settleFrame(wrapper);
}

async function openSurfaceTab(wrapper: any) {
  const tab = wrapper
    .findAll("button")
    .find((b: any) => b.text().includes("Surface"));
  expect(tab, "surface tab should be on screen").toBeTruthy();
  await tab!.trigger("click");
  await flushPromises();
}

function playButton(wrapper: any) {
  return wrapper
    .findAll("button")
    .find(
      (b: any) =>
        b.attributes("aria-label") === "Play" ||
        b.attributes("aria-label") === "Pause"
    );
}

describe("H5Viewer playback cap", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockSummary.mockResolvedValue(postSummary);
    mockFetch.mockResolvedValue(frameListing);
    // Only the cache reads this, and only for its byte accounting.
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    clearSurfaceCache();
    // performance.now paces playback and measures the run against the cap, so
    // faking the timers without it would leave the clock standing still.
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "Date", "performance"],
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("stops a run that has been playing past the cap, and says why", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    const play = playButton(wrapper);
    expect(play, "playback controls should be on screen").toBeTruthy();
    await play!.trigger("click");
    await flushPromises();
    expect(playButton(wrapper)!.attributes("aria-label")).toBe("Pause");

    // Turn the loop for a while inside the budget: each tick fires the pending
    // advance, and the surface reporting itself loaded schedules the next.
    for (let i = 0; i < 5; i++) {
      await turn(wrapper, 1000);
    }
    expect(
      playButton(wrapper)!.attributes("aria-label"),
      "still playing inside the budget"
    ).toBe("Pause");
    expect(wrapper.text()).not.toContain("Playback stopped");

    // Past two minutes the next advance must stop the run rather than fetch
    // another surface.
    await turn(wrapper, 2 * 60_000);
    await turn(wrapper, 1000);

    expect(
      playButton(wrapper)!.attributes("aria-label"),
      "playback should have stopped itself"
    ).toBe("Play");
    expect(wrapper.text()).toContain("Playback stopped after 2 minutes");

    // The point of the cap is bandwidth, so what matters is that nothing more
    // is pulled once it has fired — not merely that the button flipped.
    const afterStop = mockSurface.mock.calls.length;
    vi.advanceTimersByTime(60_000);
    await flushPromises();
    expect(mockSurface.mock.calls.length).toBe(afterStop);

    wrapper.unmount();
  });

  it("clears the notice and runs again when play is pressed after the cap", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    await playButton(wrapper)!.trigger("click");
    await flushPromises();
    await turn(wrapper, 2 * 60_000);
    await turn(wrapper, 1000);
    expect(wrapper.text()).toContain("Playback stopped after 2 minutes");

    await playButton(wrapper)!.trigger("click");
    await flushPromises();

    expect(playButton(wrapper)!.attributes("aria-label")).toBe("Pause");
    expect(wrapper.text()).not.toContain("Playback stopped");

    wrapper.unmount();
  });
});

describe("H5Viewer playback resolution cap", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockSummary.mockResolvedValue(postSummary);
    mockFetch.mockResolvedValue(frameListing);
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    clearSurfaceCache();
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "Date", "performance"],
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // A step the user picked used to survive into playback, so a FileSystem box
  // could be asked to cut five million triangles a frame for a viewer showing
  // each one for a second.
  //
  // Ultra is where a still lands now, so selecting it expresses no preference
  // on its own — the step has to be moved off the default first for the choice
  // to be the user's rather than the viewer's.
  it("drops a hand-picked ultra step when playback starts", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    const select = wrapper.find('select[aria-label="Detail"]');
    expect(select.exists(), "detail control should be on screen").toBe(true);
    await select.setValue("medium");
    await select.setValue("ultra");
    expect((select.element as HTMLSelectElement).value).toBe("ultra");

    await playButton(wrapper)!.trigger("click");
    await flushPromises();

    expect((select.element as HTMLSelectElement).value).toBe("high");
    const ultra = select
      .findAll("option")
      .find((o: any) => o.text() === "Ultra");
    expect(ultra!.attributes("disabled")).toBeDefined();

    wrapper.unmount();
  });

  // The still lands at full detail and nobody has said otherwise, so the first
  // press of play is free to take the cheap step — the wire sets the frame
  // rate, and a surface shown for a second cannot carry detail that costs
  // seconds to arrive.
  it("drops an untouched default to the playback step on first play", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    const select = wrapper.find('select[aria-label="Detail"]');
    expect((select.element as HTMLSelectElement).value).toBe("ultra");

    await playButton(wrapper)!.trigger("click");
    await flushPromises();

    expect((select.element as HTMLSelectElement).value).toBe("low");

    wrapper.unmount();
  });

  // Below the ceiling the choice is still the user's, and playback leaves it be
  // rather than forcing the low step on someone who asked for more.
  it("leaves a step inside the ceiling alone", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    const select = wrapper.find('select[aria-label="Detail"]');
    await select.setValue("medium");
    await playButton(wrapper)!.trigger("click");
    await flushPromises();

    expect((select.element as HTMLSelectElement).value).toBe("medium");

    wrapper.unmount();
  });
});

// A case that is still solving keeps writing while it is being watched. The
// running player re-reads the listing and takes on what has settled, without
// a control of its own.
describe("H5Viewer live frames", () => {
  const listingOf = (...items: { name: string; size: number }[]) => ({
    items: items.map((i) => ({ ...i, isDir: false })),
  });

  const three = [
    { name: "post000001_+1.00000e+00.h5", size: 1024 },
    { name: "post000002_+2.00000e+00.h5", size: 1024 },
    { name: "post000003_+3.00000e+00.h5", size: 1024 },
  ];

  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockSummary.mockResolvedValue(postSummary);
    mockFetch.mockResolvedValue(listingOf(...three));
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    clearSurfaceCache();
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "Date", "performance"],
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: false,
    });
  });

  const startPlaying = async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);
    await playButton(wrapper)!.trigger("click");
    await flushPromises();
    return wrapper;
  };

  it("takes on a new output once its size has settled", async () => {
    const wrapper = await startPlaying();
    expect(wrapper.text()).toContain("1/3");

    // Appears mid-write. One sighting is not enough to read it.
    mockFetch.mockResolvedValue(
      listingOf(...three, { name: "post000004_+4.00000e+00.h5", size: 400 })
    );
    await turn(wrapper, 30_000);
    expect(wrapper.text()).toContain("/3");

    // Still growing.
    mockFetch.mockResolvedValue(
      listingOf(...three, { name: "post000004_+4.00000e+00.h5", size: 900 })
    );
    await turn(wrapper, 30_000);
    expect(wrapper.text()).toContain("/3");

    // Unchanged across a poll: the write finished, so it joins the loop.
    await turn(wrapper, 30_000);
    expect(wrapper.text()).toContain("/4");

    wrapper.unmount();
  });

  it("keeps the frame on screen when the listing shifts under it", async () => {
    const wrapper = await startPlaying();

    // Move the loop on and leave the frame unsettled: the player then waits on
    // it while the poll chain carries on, which is the window where a merge
    // lands under a display that is standing still.
    vi.advanceTimersByTime(1000);
    await flushPromises();
    expect(wrapper.text()).toContain("2.00000 · 2/3");

    // A leg written before this one turns up late, so it sorts in ahead of
    // everything on screen rather than appending.
    const earlier = { name: "post000000_+5.00000e-01.h5", size: 1024 };
    mockFetch.mockResolvedValue(listingOf(earlier, ...three));
    vi.advanceTimersByTime(30_000);
    await flushPromises();
    vi.advanceTimersByTime(30_000);
    await flushPromises();

    // Same frame still on screen; only its number moved, to account for the
    // one that slotted in front of it.
    expect(wrapper.text()).toContain("2.00000 · 3/4");

    wrapper.unmount();
  });

  it("stops at the cap even while outputs are still landing", async () => {
    const wrapper = await startPlaying();

    for (let i = 0; i < 4; i++) {
      mockFetch.mockResolvedValue(
        listingOf(...three, {
          name: `post00000${4 + i}_+${4 + i}.00000e+00.h5`,
          size: 1024,
        })
      );
      await turn(wrapper, 30_000);
    }
    await turn(wrapper, 1000);

    expect(
      playButton(wrapper)!.attributes("aria-label"),
      "a live case does not buy more than one sitting"
    ).toBe("Play");
    expect(wrapper.text()).toContain("Playback stopped after 2 minutes");

    wrapper.unmount();
  });

  it("stops polling once playback stops", async () => {
    const wrapper = await startPlaying();
    await turn(wrapper, 30_000);

    await playButton(wrapper)!.trigger("click");
    await flushPromises();
    mockFetch.mockClear();

    vi.advanceTimersByTime(5 * 60_000);
    await flushPromises();
    expect(mockFetch).not.toHaveBeenCalled();

    wrapper.unmount();
  });
});

describe("H5Viewer pauses when nobody is looking", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockSummary.mockResolvedValue(postSummary);
    mockFetch.mockResolvedValue(frameListing);
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    clearSurfaceCache();
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "Date", "performance"],
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: false,
    });
  });

  const play = async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;
    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);
    await playButton(wrapper)!.trigger("click");
    await flushPromises();
    await turn(wrapper, 1000);
    return wrapper;
  };

  // Each frame is a surface cut on a box that is also running the solve, so
  // a backgrounded tab must not keep buying them.
  it("stops on a hidden tab and pulls nothing more", async () => {
    const wrapper = await play();
    expect(playButton(wrapper)!.attributes("aria-label")).toBe("Pause");

    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
    await flushPromises();

    expect(playButton(wrapper)!.attributes("aria-label")).toBe("Play");

    const afterHide = mockSurface.mock.calls.length;
    vi.advanceTimersByTime(60_000);
    await flushPromises();
    expect(mockSurface.mock.calls.length).toBe(afterHide);

    wrapper.unmount();
  });

  it("stops when the window loses focus with the tab still selected", async () => {
    const wrapper = await play();

    window.dispatchEvent(new Event("blur"));
    await flushPromises();

    expect(playButton(wrapper)!.attributes("aria-label")).toBe("Play");

    wrapper.unmount();
  });

  // Coming back is the user's call: it must not resume a stream on its own.
  it("stays stopped when the page comes back", async () => {
    const wrapper = await play();
    window.dispatchEvent(new Event("blur"));
    await flushPromises();

    window.dispatchEvent(new Event("focus"));
    document.dispatchEvent(new Event("visibilitychange"));
    await flushPromises();

    expect(playButton(wrapper)!.attributes("aria-label")).toBe("Play");

    wrapper.unmount();
  });
});

// A mesh can be perfectly fine to look at and far too expensive to play: every
// frame is a fresh pass over the whole face table to find the boundary, which
// no detail step reduces. The still stays; the transport goes.
describe("H5Viewer playback on a mesh too large to sequence", () => {
  // The file that prompted this: 27.4M cells, 84,125,742 faces, whose wetted
  // surface is only 1,366,195 of them.
  const hugeSummary = {
    ...postSummary,
    streams: [{ ...postSummary.streams[0], cells: 27412975, faces: 84125742 }],
    parcels: undefined,
  };

  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockSummary.mockResolvedValue(hugeSummary);
    mockFetch.mockResolvedValue(frameListing);
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    clearSurfaceCache();
  });

  it("offers no transport, and says why rather than leaving a gap", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    expect(playButton(wrapper)).toBeUndefined();
    expect(wrapper.text()).toContain("Too large to play back");
    // The still itself is untouched — this is a playback limit, not a refusal.
    expect(
      wrapper.findComponent({ name: "BoundarySurface" }).exists(),
      "the surface should still be drawn"
    ).toBe(true);

    wrapper.unmount();
  });

  // The listing is only ever wanted for the transport.
  it("does not pay for the directory listing it cannot use", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    expect(mockFetch).not.toHaveBeenCalled();

    wrapper.unmount();
  });

  // Space and the arrows drive the same transport, so they have to be behind
  // the same gate or they would play a sequence with no player on screen.
  it("ignores the playback keys on the surface tab", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    window.dispatchEvent(new KeyboardEvent("keydown", { code: "Space" }));
    window.dispatchEvent(new KeyboardEvent("keydown", { code: "ArrowRight" }));
    await flushPromises();

    expect(mockFetch).not.toHaveBeenCalled();
    expect(playButton(wrapper)).toBeUndefined();

    wrapper.unmount();
  });

  // A parcel cloud never touches the mesh — it is flat arrays of positions —
  // so the same file that cannot play its surface plays its spray fine.
  it("still plays parcels on the same file", async () => {
    mockSummary.mockResolvedValue({
      ...hugeSummary,
      streams: [
        {
          ...hugeSummary.streams[0],
          parcels: [
            {
              name: "LIQPARCEL_1",
              path: "STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1",
              count: 115553,
              variables: ["TEMP"],
              hasCoords: true,
            },
          ],
        },
      ],
    });

    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();

    const tab = wrapper
      .findAll("button")
      .find((b: any) => b.text().includes("Parcels"));
    expect(tab, "parcels tab should be on screen").toBeTruthy();
    await tab!.trigger("click");
    await flushPromises();

    expect(
      mockFetch,
      "parcels still need the sibling listing"
    ).toHaveBeenCalled();
    expect(playButton(wrapper), "parcel playback should survive").toBeTruthy();

    wrapper.unmount();
  });
});
