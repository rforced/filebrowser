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
          username: "Username",
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
      execute: false,
      modify: true,
      move: true,
      rename: true,
      share: true,
      shell: false,
      upload: true,
      ...overrides,
    },
    commands: [],
    rules: [],
    lockPassword: false,
    hideDotfiles: false,
    singleClick: false,
    redirectAfterCopyMove: false,
    dateFormat: false,
    viewMode: "list",
    aceEditorTheme: "",
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

  it("shows username column for admin users", async () => {
    const shares: Share[] = [
      { hash: "s1", path: "/file.txt", expire: 0, userID: 2 },
    ];
    mockShareList.mockResolvedValue(shares);
    mockUsersGetAll.mockResolvedValue([{ id: 2, username: "alice" }]);

    const authStore = useAuthStore();
    authStore.user = makeUser({ admin: true });

    const wrapper = mountShares();
    await flushPromises();

    expect(wrapper.text()).toContain("Username");
    expect(wrapper.text()).toContain("alice");
  });

  it("does not show username column for non-admin users", async () => {
    const shares: Share[] = [{ hash: "s1", path: "/file.txt", expire: 0 }];
    mockShareList.mockResolvedValue(shares);

    const authStore = useAuthStore();
    authStore.user = makeUser({ admin: false });

    const wrapper = mountShares();
    await flushPromises();

    expect(wrapper.text()).not.toContain("Username");
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

  it("each share row has a delete button", async () => {
    const shares: Share[] = [
      { hash: "d1", path: "/deletable.txt", expire: 0 },
      { hash: "d2", path: "/also-deletable.txt", expire: 0 },
    ];
    mockShareList.mockResolvedValue(shares);

    const authStore = useAuthStore();
    authStore.user = makeUser();

    const wrapper = mountShares();
    await flushPromises();

    const deleteButtons = wrapper.findAll("button.action");
    // Each row has 2 action buttons (delete + copy), so 4 total for 2 rows
    expect(deleteButtons.length).toBe(4);

    const deleteIcons = wrapper
      .findAll("button.action i.material-icons")
      .filter((el) => el.text() === "delete");
    expect(deleteIcons.length).toBe(2);
  });
});
