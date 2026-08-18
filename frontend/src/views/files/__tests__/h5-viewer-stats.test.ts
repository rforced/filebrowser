import { describe, expect, it, vi, beforeEach } from "vitest";
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

vi.mock("@/api", () => ({
  files: { getDownloadURL: () => "/download" },
}));

const mockSummary = vi.fn();
const mockStats = vi.fn();

vi.mock("@/api/h5", () => ({
  summary: (...args: any[]) => mockSummary(...args),
  stats: (...args: any[]) => mockStats(...args),
  subsetURL: () => "/subset",
}));

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/files/case/post000001_+1.h5" }),
  useRouter: () => ({ push: vi.fn() }),
}));

import { useFileStore } from "@/stores/file";
import H5Viewer from "../H5Viewer.vue";

const variable = (name: string) => ({
  name,
  path: `STREAM_00/CELL_CENTER_DATA/${name}`,
  type: "float32",
  dims: [4],
  bytes: 16,
});

// Two post files from the same case: same variables, same dataset paths,
// different numbers. That identity is what makes a stale response dangerous —
// it files perfectly cleanly under the new file's rows.
const summaryFor = (name: string) => ({
  name,
  size: 1024,
  kind: "post" as const,
  streams: [
    {
      name: "STREAM_00",
      cells: 4,
      variables: [variable("TEMPERATURE")],
    },
  ],
});

const statsEntry = (mean: number) => ({
  path: "STREAM_00/CELL_CENTER_DATA/TEMPERATURE",
  name: "TEMPERATURE",
  type: "float32",
  count: 4,
  min: mean,
  max: mean,
  mean,
  nan: 0,
  inf: 0,
  finite: 4,
});

function mountViewer() {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: {
      en: {
        h5View: {
          computeStats: "Compute stats",
          statsFailed: "Could not compute statistics: {message}",
          variables: "Variables",
        },
      },
    },
  });

  return mount(H5Viewer, {
    global: {
      plugins: [i18n],
      directives: { tooltip: {} },
      stubs: {
        ParcelCloud: true,
        RouterLink: true,
      },
    },
  });
}

describe("H5Viewer stats", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockSummary.mockReset();
    mockStats.mockReset();
  });

  it("drops a stats batch that outlived the file it was asked for", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.h5" } as any;

    mockSummary.mockImplementation(async (path: string) => summaryFor(path));

    // The first file's batch hangs until we let it go, so it can be made to
    // land after the viewer has already moved on to the second file.
    let releaseFirst: (value: any) => void = () => {};
    mockStats.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          releaseFirst = resolve;
        })
    );

    const wrapper = mountViewer();
    await flushPromises();

    const computeStats = wrapper.find("button.btn-white");
    expect(computeStats.exists()).toBe(true);
    await computeStats.trigger("click");

    // Switch files while that batch is still in flight.
    fileStore.req = { path: "/case/post000002_+2.h5" } as any;
    await flushPromises();

    releaseFirst([statsEntry(9999)]);
    await flushPromises();

    // The abandoned numbers must not appear against the new file's variables.
    expect(wrapper.text()).not.toContain("9999");

    // And the viewer is still usable: a stats run for the new file works.
    mockStats.mockResolvedValueOnce([statsEntry(712.625)]);
    await wrapper.find("button.btn-white").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("712.625");

    wrapper.unmount();
  });

  it("keeps the viewer on screen when a stats batch fails", async () => {
    const fileStore = useFileStore();
    fileStore.req = { path: "/case/post000001_+1.h5" } as any;

    mockSummary.mockImplementation(async (path: string) => summaryFor(path));
    mockStats.mockRejectedValueOnce(new Error("boom"));

    const wrapper = mountViewer();
    await flushPromises();
    await wrapper.find("button.btn-white").trigger("click");
    await flushPromises();

    // The manifest is what the user came for; a failed statistics pass reports
    // itself without taking the file's contents off the screen.
    expect(wrapper.text()).toContain("TEMPERATURE");
    expect(wrapper.text()).toContain("boom");

    wrapper.unmount();
  });
});
