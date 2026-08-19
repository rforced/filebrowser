import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
const mockParcels = vi.fn();
vi.mock("@/api/h5", () => ({
  summary: (...args: any[]) => mockSummary(...args),
  surface: (...args: any[]) => mockSurface(...args),
  parcels: (...args: any[]) => mockParcels(...args),
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

const variable = (name: string) => ({
  name,
  path: `STREAM_00/CELL_CENTER_DATA/${name}`,
  type: "float32",
  dims: [4],
  bytes: 16,
});

// A post file with a boundary surface and two cell fields, and no parcels —
// the shape that puts every tab on screen with one of them empty.
const postSummary = {
  name: "post000001_+1.00000e+00.h5",
  size: 1024,
  kind: "post" as const,
  boundaries: [{ id: 1, name: "PISTON", elements: 4 }],
  streams: [
    {
      name: "STREAM_00",
      cells: 4,
      faces: 8,
      vertices: 8,
      variables: [variable("TEMPERATURE"), variable("PRESSURE")],
    },
  ],
};

// The same file with a spray in it, so both 3D tabs have something to draw and
// playback has a choice of which to stream.
const sprayedSummary = {
  ...postSummary,
  streams: [
    {
      ...postSummary.streams[0],
      parcels: [
        {
          name: "LIQPARCEL_1",
          path: "STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1",
          count: 3,
          hasCoords: true,
          variables: ["RADIUS", "TEMP"],
        },
      ],
    },
  ],
};

// A restart: no mesh faces, no boundaries, no parcels. Every 3D tab is empty.
const restartSummary = {
  name: "restart0001.rst",
  size: 1024,
  kind: "restart" as const,
  boundaries: [],
  streams: [
    {
      name: "STREAM_00",
      cells: 4,
      variables: [variable("TEMPERATURE")],
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
          variables: "Variables",
          surface: "Surface",
          parcels: "Parcels",
          boundaries: "Boundaries",
          noSurface: "This file holds no boundary surface.",
          noParcels: "This file holds no parcels.",
          noBoundaries: "This file names no boundaries.",
          colourBy: "Colour by",
          byBoundary: "By boundary",
          representation: "Show as",
          resolution: "Detail",
          nextFrame: "Next frame",
          prevFrame: "Previous frame",
          frame: "Frame",
        },
        buttons: { play: "Play", pause: "Pause" },
      },
    },
  });

  return mount(H5Viewer, {
    global: {
      plugins: [i18n],
      directives: { tooltip: {} },
      stubs: { ParcelCloud: true, BoundarySurface: true, RouterLink: true },
    },
  });
}

const tabButton = (wrapper: any, label: string) =>
  wrapper.findAll("button").find((b: any) => b.text().trim() === label);

const openTab = async (wrapper: any, label: string) => {
  const tab = tabButton(wrapper, label);
  expect(tab, `${label} tab should be on screen`).toBeTruthy();
  await tab!.trigger("click");
  await flushPromises();
};

describe("H5Viewer tabs", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockFetch.mockResolvedValue(frameListing);
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    clearSurfaceCache();

    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;
  });

  it("offers every tab even for a file that carries none of them", async () => {
    mockSummary.mockResolvedValue(restartSummary);

    const wrapper = mountViewer();
    await flushPromises();

    for (const label of ["Variables", "Surface", "Parcels", "Boundaries"]) {
      expect(tabButton(wrapper, label), `${label} tab`).toBeTruthy();
    }

    wrapper.unmount();
  });

  it("says what is missing instead of hiding the tab", async () => {
    mockSummary.mockResolvedValue(restartSummary);

    const wrapper = mountViewer();
    await flushPromises();

    await openTab(wrapper, "Surface");
    expect(wrapper.text()).toContain("This file holds no boundary surface.");

    await openTab(wrapper, "Parcels");
    expect(wrapper.text()).toContain("This file holds no parcels.");

    await openTab(wrapper, "Boundaries");
    expect(wrapper.text()).toContain("This file names no boundaries.");

    wrapper.unmount();
  });

  it("keeps the surface tab working for a file that does carry one", async () => {
    mockSummary.mockResolvedValue(postSummary);

    const wrapper = mountViewer();
    await flushPromises();
    await openTab(wrapper, "Parcels");
    expect(wrapper.text()).toContain("This file holds no parcels.");

    await openTab(wrapper, "Surface");
    expect(wrapper.text()).not.toContain(
      "This file holds no boundary surface."
    );
    expect(
      wrapper.findComponent({ name: "BoundarySurface" }).exists(),
      "the surface view should mount for a file with a mesh"
    ).toBe(true);

    wrapper.unmount();
  });
});

// One player, one timeline, but only the tab you are looking at streams. The
// hidden view stays frozen where it was and catches up when its tab is opened,
// which is why the two look synchronised without both being fetched.
describe("H5Viewer playback drives only the visible view", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockFetch.mockReset();
    mockSurface.mockReset();
    mockParcels.mockReset();
    mockSummary.mockResolvedValue(sprayedSummary);
    mockFetch.mockResolvedValue(frameListing);
    mockSurface.mockResolvedValue({
      positions: new Float32Array(new ArrayBuffer(64)),
    });
    mockParcels.mockResolvedValue({ points: [], count: 0, range: [0, 1] });
    clearSurfaceCache();

    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;

    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "Date", "performance"],
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("leaves the parcel cloud where it was while the surface plays", async () => {
    const wrapper = mountViewer();
    await flushPromises();

    // Open parcels first so the cloud is mounted and has a frame of its own,
    // then leave it for the surface: a frozen view still on screen behind the
    // active tab is exactly the case that could double the streaming.
    await openTab(wrapper, "Parcels");
    await openTab(wrapper, "Surface");

    const cloud = () => wrapper.findComponent({ name: "ParcelCloud" });
    expect(cloud().exists(), "the cloud stays mounted across tabs").toBe(true);
    const frozenAt = cloud().props("path");
    const parcelCallsBefore = mockParcels.mock.calls.length;

    const play = wrapper
      .findAll("button")
      .find((b: any) => b.attributes("aria-label") === "Play");
    expect(play, "playback controls should be on screen").toBeTruthy();
    await play!.trigger("click");
    await flushPromises();

    const view = () => wrapper.findComponent({ name: "BoundarySurface" });
    for (let i = 0; i < 4; i++) {
      vi.advanceTimersByTime(1000);
      await flushPromises();
      view().vm.$emit("loaded", [0, 1]);
      await flushPromises();
    }

    expect(
      view().props("path"),
      "the surface should have moved through the sequence"
    ).not.toBe(frozenAt);
    expect(
      cloud().props("path"),
      "the hidden cloud must stay on the frame it was left at"
    ).toBe(frozenAt);
    expect(
      mockParcels.mock.calls.length,
      "no parcel frame should be fetched while the surface plays"
    ).toBe(parcelCallsBefore);

    wrapper.unmount();
  });
});

describe("H5Viewer colour ramp lock", () => {
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

    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.00000e+00.h5" } as any;
  });

  // Stepping a frame takes the lock from the range the settled frame reported.
  // Between picking a new field and its first frame arriving, that range still
  // belongs to the old field, and a temperature scale drawn over a pressure
  // field clamps the whole wall to one end of the ramp.
  it("does not lock a new field to the range measured from the old one", async () => {
    const wrapper = mountViewer();
    await flushPromises();

    const surfaceTab = wrapper
      .findAll("button")
      .find((b: any) => b.text().trim() === "Surface");
    await surfaceTab!.trigger("click");
    await flushPromises();

    const colourBy = wrapper
      .findAll("select")
      .find((s: any) => s.attributes("aria-label") === "Colour by");
    expect(colourBy, "the colour-by control should be on screen").toBeTruthy();

    await colourBy!.setValue("TEMPERATURE");
    await flushPromises();

    const view = () => wrapper.findComponent({ name: "BoundarySurface" });
    view().vm.$emit("loaded", [300, 900]);
    await flushPromises();

    // Switching field clears the lock, but the last measured range is still
    // temperature's until pressure's first frame lands.
    await colourBy!.setValue("PRESSURE");
    await flushPromises();

    const next = wrapper
      .findAll("button")
      .find((b: any) => b.attributes("aria-label") === "Next frame");
    expect(next, "the frame player should be on screen").toBeTruthy();
    await next!.trigger("click");
    await flushPromises();

    expect(
      view().props("range"),
      "pressure must not be drawn against temperature's scale"
    ).toBeNull();

    // Once pressure reports its own range, stepping locks to that.
    view().vm.$emit("loaded", [1e5, 4e5]);
    await flushPromises();
    await next!.trigger("click");
    await flushPromises();

    expect(view().props("range")).toEqual([1e5, 4e5]);

    wrapper.unmount();
  });
});
