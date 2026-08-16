import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { useAuthStore } from "@/stores/auth";
import { useLayoutStore } from "@/stores/layout";

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

vi.mock("@/utils/auth", () => ({
  renew: vi.fn(),
  logout: vi.fn(),
}));

// Mock the API modules used by Shares.vue
const mockShareList = vi.fn();
const mockGetShareURL = vi.fn();
const mockUsersGetAll = vi.fn();

vi.mock("@/api", () => ({
  share: {
    list: (...args: any[]) => mockShareList(...args),
    remove: vi.fn(),
    getShareURL: (...args: any[]) => mockGetShareURL(...args),
  },
  users: {
    getAll: (...args: any[]) => mockUsersGetAll(...args),
  },
}));

// Mock clipboard utility used by Shares.vue
vi.mock("@/utils/clipboard", () => ({
  copy: vi.fn().mockResolvedValue(undefined),
}));

import Shares from "../Shares.vue";

function createI18nPlugin() {
  return createI18n({
    legacy: false,
    locale: "en",
    messages: {
      en: {
        permanent: "Permanent",
        settings: {
          shareManagement: "Share Management",
          path: "Path",
          shareDuration: "Share Duration",
          owner: "Owner",
          shareDeleted: "Share deleted!",
        },
        files: {
          lonely: "Nothing here",
        },
        buttons: {
          delete: "Delete",
          copyToClipboard: "Copy to clipboard",
        },
        success: {
          linkCopied: "Link copied!",
        },
      },
    },
  });
}

function makeUser(overrides: Partial<Permissions> = {}): IUser {
  return {
    id: 1,
    username: "testuser",
    password: "",
    scope: "/",
    locale: "en",
    perm: {
      admin: false,
      copy: true,
      create: true,
      delete: true,
      download: true,
      modify: true,
      move: true,
      rename: true,
      share: true,
      upload: true,
      ...overrides,
    },
    rules: [],
    lockPassword: false,
    hideDotfiles: false,
    singleClick: false,
    redirectAfterCopyMove: false,
    dateFormat: false,
    viewMode: "list",
  };
}

function mountShares() {
  const i18n = createI18nPlugin();
  return mount(Shares, {
    global: {
      plugins: [i18n],
      provide: {
        $showError: vi.fn(),
        $showSuccess: vi.fn(),
      },
    },
  });
}

describe("Shares.vue", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.restoreAllMocks();

    // Default: return empty arrays unless overridden
    mockShareList.mockResolvedValue([]);
    mockUsersGetAll.mockResolvedValue([]);
    mockGetShareURL.mockImplementation(
      (share: Share) => `http://localhost/test/share/${share.hash}`
    );
  });

  it("renders share items in the table when shares exist", async () => {
    const shares: Share[] = [
      { hash: "abc123", path: "/docs/readme.md", expire: 1700000000 },
      { hash: "def456", path: "/images/photo.png", expire: 0 },
    ];
    mockShareList.mockResolvedValue(shares);

    const authStore = useAuthStore();
    authStore.user = makeUser();

    const wrapper = mountShares();
    await flushPromises();

    const rows = wrapper.findAll("table tr");
    // 1 header row + 2 data rows
    expect(rows.length).toBe(3);

    // First share row
    expect(rows[1].text()).toContain("/docs/readme.md");

    // Second share row — permanent
    expect(rows[2].text()).toContain("/images/photo.png");
    expect(rows[2].text()).toContain("Permanent");
  });

  it("shows empty state when no shares exist", async () => {
    mockShareList.mockResolvedValue([]);

    const authStore = useAuthStore();
    authStore.user = makeUser();

    const wrapper = mountShares();
    await flushPromises();

    expect(wrapper.find("table").exists()).toBe(false);
    expect(wrapper.text()).toContain("Nothing here");
  });

  it("renders links pointing to the correct share URL", async () => {
    const shares: Share[] = [
      { hash: "link1", path: "/test/file.txt", expire: 0 },
    ];
    mockShareList.mockResolvedValue(shares);

    const authStore = useAuthStore();
    authStore.user = makeUser();

    const wrapper = mountShares();
    await flushPromises();

    const link = wrapper.find("table a");
    expect(link.exists()).toBe(true);
    expect(link.attributes("href")).toContain("share/link1");
  });

  it("names the owner of every share, to admins and plain users alike", async () => {
    const shares: Share[] = [
      {
        hash: "s1",
        path: "/file.txt",
        expire: 0,
        userID: 2,
        username: "alice",
      },
    ];
    mockShareList.mockResolvedValue(shares);

    for (const admin of [true, false]) {
      const authStore = useAuthStore();
      authStore.user = makeUser({ admin });

      const wrapper = mountShares();
      await flushPromises();

      expect(wrapper.text()).toContain("Owner");
      expect(wrapper.text()).toContain("alice");
    }

    expect(mockUsersGetAll).not.toHaveBeenCalled();
  });

  it("renders multiple shares preserving order", async () => {
    const shares: Share[] = [
      { hash: "a", path: "/first.txt", expire: 0 },
      { hash: "b", path: "/second.txt", expire: 0 },
      { hash: "c", path: "/third.txt", expire: 0 },
    ];
    mockShareList.mockResolvedValue(shares);

    const authStore = useAuthStore();
    authStore.user = makeUser();

    const wrapper = mountShares();
    await flushPromises();

    const rows = wrapper.findAll("table tr");
    expect(rows.length).toBe(4); // 1 header + 3 data

    expect(rows[1].text()).toContain("/first.txt");
    expect(rows[2].text()).toContain("/second.txt");
    expect(rows[3].text()).toContain("/third.txt");
  });

  it("sets loading to false after shares are fetched", async () => {
    mockShareList.mockResolvedValue([]);

    const authStore = useAuthStore();
    authStore.user = makeUser();

    const layoutStore = useLayoutStore();

    mountShares();
    await flushPromises();

    expect(layoutStore.loading).toBe(false);
  });

  it("offers a delete button on every share, whoever owns it", async () => {
    const shares: Share[] = [
      { hash: "mine", path: "/mine.txt", expire: 0, userID: 1 },
      { hash: "theirs", path: "/theirs.txt", expire: 0, userID: 2 },
    ];
    mockShareList.mockResolvedValue(shares);

    const authStore = useAuthStore();
    authStore.user = makeUser({ admin: false });

    const wrapper = mountShares();
    await flushPromises();

    const rows = wrapper.findAll("table tr");
    expect(rows.length).toBe(3);
    expect(rows[2].text()).toContain("/theirs.txt");

    expect(rows[1].findAll("button.action i.fa-trash").length).toBe(1);
    expect(rows[2].findAll("button.action i.fa-trash").length).toBe(1);
    expect(wrapper.findAll("button.action").length).toBe(4);
  });
});
