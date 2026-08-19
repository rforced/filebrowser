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
          variables: "Variables",
          playbackCapped:
            "Playback stopped after {minutes} minutes. Press play to carry on.",
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
  it("drops a hand-picked ultra step when playback starts", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    const wrapper = mountViewer();
    await flushPromises();
    await openSurfaceTab(wrapper);

    const select = wrapper.find('select[aria-label="Detail"]');
    expect(select.exists(), "detail control should be on screen").toBe(true);
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
