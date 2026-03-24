import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { useFileStore } from "@/stores/file";

dayjs.extend(relativeTime);

vi.mock("@/utils/constants", () => ({
  baseURL: "/test",
  origin: "http://localhost",
  name: "Test",
  staticURL: "/static",
  disableExternal: false,
  disableUsedPercentage: false,
  recaptcha: "",
  recaptchaKey: "",
  signup: false,
  version: "0.0.0",
  noAuth: false,
  authMethod: "password",
  logoutPage: "",
  loginPage: true,
  theme: "light",
  enableThumbs: false,
  resizePreview: false,
  enableExec: false,
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

vi.mock("@/utils/auth", () => ({
  renew: vi.fn(),
  logout: vi.fn(),
}));

const mockDirSize = vi.fn();

vi.mock("@/api", () => ({
  files: {
    dirSize: (...args: any[]) => mockDirSize(...args),
    checksum: vi.fn(),
  },
}));

import Info from "../Info.vue";

function createI18nPlugin() {
  return createI18n({
    legacy: true,
    locale: "en",
    messages: {
      en: {
        buttons: {
          ok: "OK",
        },
        prompts: {
          fileInfo: "File information",
          displayName: "Display Name:",
          size: "Size",
          lastModified: "Last Modified",
          numberFiles: "Number of files",
          numberDirs: "Number of directories",
          filesSelected: "{count} files selected.",
          show: "Show",
          calculateSize: "Calculate",
          calculating: "Calculating...",
          resolution: "Resolution",
        },
      },
    },
  });
}

function makeDirectoryResource(overrides: Partial<Resource> = {}): Resource {
  return {
    name: "test-folder",
    path: "/test-folder",
    size: 0,
    isDir: true,
    type: "",
    modified: new Date().toISOString(),
    mode: 0o755,
    extension: "",
    url: "/files/test-folder/",
    items: [],
    numDirs: 2,
    numFiles: 5,
    sorting: { by: "name", asc: true },
    ...overrides,
  } as Resource;
}

interface MountOptions {
  route?: { path: string };
  storeSetup?: (store: ReturnType<typeof useFileStore>) => void;
  showError?: ReturnType<typeof vi.fn>;
}

function mountInfo(opts: MountOptions = {}) {
  const {
    route = { path: "/files/test-folder/" },
    storeSetup,
    showError = vi.fn(),
  } = opts;

  const pinia = createPinia();
  setActivePinia(pinia);

  if (storeSetup) {
    const fileStore = useFileStore();
    storeSetup(fileStore);
  }

  const i18n = createI18nPlugin();
  return {
    wrapper: mount(Info, {
      global: {
        plugins: [pinia, i18n],
        provide: {
          $showError: showError,
        },
        mocks: {
          $route: route,
        },
      },
    }),
    showError,
  };
}

describe("Info.vue", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    mockDirSize.mockResolvedValue({ size: 0, numFiles: 0, numDirs: 0 });
  });

  describe("directory size calculation", () => {
    it("shows Calculate link for a directory", async () => {
      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      expect(calcLink.exists()).toBe(true);
      expect(calcLink.text()).toBe("Calculate");
    });

    it("calls dirSize API when Calculate is clicked", async () => {
      mockDirSize.mockResolvedValue({ size: 2048, numFiles: 3, numDirs: 1 });

      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await flushPromises();

      expect(mockDirSize).toHaveBeenCalledOnce();
    });

    it("displays calculated size after clicking Calculate", async () => {
      mockDirSize.mockResolvedValue({
        size: 1048576,
        numFiles: 10,
        numDirs: 2,
      });

      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await flushPromises();

      // After calculation, the "Calculate" link should be gone
      const links = wrapper.findAll("a");
      const calcLinks = links.filter((l) => l.text() === "Calculate");
      expect(calcLinks).toHaveLength(0);

      // Should display the formatted size (1 MiB)
      expect(wrapper.text()).toContain("1");
    });

    it("displays file and directory counts after calculation", async () => {
      mockDirSize.mockResolvedValue({ size: 5120, numFiles: 15, numDirs: 4 });

      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await flushPromises();

      expect(wrapper.text()).toContain("15");
      expect(wrapper.text()).toContain("4");
      expect(wrapper.text()).toContain("Number of files");
      expect(wrapper.text()).toContain("Number of directories");
    });

    it("does not show file/dir counts before calculation", async () => {
      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const text = wrapper.text();
      expect(text).toContain("Calculate");
    });

    it("shows Calculating... text while loading", async () => {
      let resolveSize!: (value: any) => void;
      mockDirSize.mockReturnValue(
        new Promise((resolve) => {
          resolveSize = resolve;
        })
      );

      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await wrapper.vm.$nextTick();

      expect(wrapper.text()).toContain("Calculating...");

      resolveSize({ size: 100, numFiles: 1, numDirs: 0 });
      await flushPromises();

      expect(wrapper.text()).not.toContain("Calculating...");
    });

    it("uses the route path when no item is selected", async () => {
      mockDirSize.mockResolvedValue({ size: 0, numFiles: 0, numDirs: 0 });

      const { wrapper } = mountInfo({
        route: { path: "/files/my-folder/" },
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await flushPromises();

      expect(mockDirSize).toHaveBeenCalledWith("/files/my-folder/");
    });

    it("uses the selected item URL when an item is selected", async () => {
      mockDirSize.mockResolvedValue({ size: 512, numFiles: 2, numDirs: 1 });

      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = makeDirectoryResource({
            items: [
              {
                name: "subfolder",
                isDir: true,
                url: "/files/test-folder/subfolder/",
                size: 0,
                modified: new Date().toISOString(),
                mode: 0o755,
                extension: "",
                type: "",
                index: 0,
              } as any,
            ],
          });
          store.selected = [0];
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await flushPromises();

      expect(mockDirSize).toHaveBeenCalledWith("/files/test-folder/subfolder/");
    });

    it("calls $showError when dirSize fails", async () => {
      const apiError = new Error("Network error");
      mockDirSize.mockRejectedValue(apiError);

      const showError = vi.fn();
      const { wrapper } = mountInfo({
        showError,
        storeSetup: (store) => {
          store.req = makeDirectoryResource();
        },
      });
      await flushPromises();

      const calcLink = wrapper.find("a");
      await calcLink.trigger("click");
      await flushPromises();

      expect(showError).toHaveBeenCalledWith(apiError);
    });

    it("does not show Calculate link for a file", async () => {
      const { wrapper } = mountInfo({
        storeSetup: (store) => {
          store.req = {
            name: "test.txt",
            path: "/test.txt",
            size: 100,
            isDir: false,
            type: "text",
            modified: new Date().toISOString(),
            mode: 0o644,
            extension: ".txt",
            url: "/files/test.txt",
          } as Resource;
        },
      });
      await flushPromises();

      expect(wrapper.text()).not.toContain("Calculate");
    });
  });
});
