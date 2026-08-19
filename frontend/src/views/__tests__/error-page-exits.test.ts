import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount, flushPromises, RouterLinkStub } from "@vue/test-utils";
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

const mockFilesFetch = vi.fn();
const mockPubFetch = vi.fn();
vi.mock("@/api", () => ({
  files: {
    fetch: (...a: any[]) => mockFilesFetch(...a),
    getDownloadURL: () => "/download",
    usage: vi.fn().mockResolvedValue({ used: 0, total: 0 }),
  },
  pub: {
    fetch: (...a: any[]) => mockPubFetch(...a),
    getDownloadURL: () => "/download",
    download: vi.fn(),
  },
  users: { update: vi.fn() },
}));

const routePath = { value: "/files/case/outputs_original/output" };
// The stores reach @/utils/auth, which builds the real router at import time,
// so the mock has to stand in for the construction helpers as well.
vi.mock("vue-router", () => ({
  useRoute: () => ({
    get path() {
      return routePath.value;
    },
    params: { path: shareParams.value },
    query: {},
  }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  createRouter: () => ({
    beforeResolve: vi.fn(),
    push: vi.fn(),
    replace: vi.fn(),
    currentRoute: { value: { path: "/", query: {} } },
  }),
  createWebHistory: () => ({}),
}));

const shareParams = { value: [] as string[] };

import { StatusError } from "@/api/utils";
import Files from "@/views/Files.vue";
import Share from "@/views/Share.vue";

function i18nPlugin() {
  return createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: {
      en: {
        errors: {
          notFound: "This location can't be reached.",
          forbidden: "You don't have permissions to access this.",
          internal: "Something really went wrong.",
          connection: "The server can't be reached.",
          shareNotFound: "This share doesn't exist or has expired.",
          tooManyAttempts: "Too many attempts. Try again in a minute.",
          backToFiles: "Back to my files",
          backToShare: "Back to the shared folder",
        },
        buttons: {
          refresh: "Refresh",
          switchView: "Switch view",
          selectMultiple: "Select multiple",
          submit: "Submit",
        },
        files: { home: "Home", loading: "Loading", files: "Files" },
        sidebar: { myFiles: "My files" },
        login: { password: "Password", wrongCredentials: "Wrong" },
      },
    },
  });
}

const mountView = (view: any) =>
  mount(view, {
    global: {
      plugins: [i18nPlugin(), createPinia()],
      directives: { tooltip: {}, focus: {} },
      components: { RouterLink: RouterLinkStub },
      stubs: {
        RouterLink: RouterLinkStub,
        FileListing: true,
        FileActions: true,
        StorageCard: true,
        HelpBox: true,
        Search: true,
        QrcodeVue: true,
        Item: true,
      },
      provide: { $showError: vi.fn(), $showSuccess: vi.fn() },
    },
  });

const linkTargets = (wrapper: any) =>
  wrapper.findAllComponents(RouterLinkStub).map((l: any) => l.props("to"));

describe("an unreachable location under /files", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockFilesFetch.mockReset();
    routePath.value = "/files/case/outputs_original/output";
    mockFilesFetch.mockRejectedValue(new StatusError("not found", 404));
  });

  it("keeps the crumbs, so every ancestor is a way back", async () => {
    const wrapper = mountView(Files);
    await flushPromises();

    expect(wrapper.text()).toContain("This location can't be reached.");

    // Built from the route alone, so they survive a fetch that did not.
    const targets = linkTargets(wrapper);
    expect(targets).toContain("/files");
    expect(targets).toContain("/files/case/");
    expect(targets).toContain("/files/case/outputs_original/");

    wrapper.unmount();
  });

  it("offers an explicit way out, which is all there is when embedded", async () => {
    const wrapper = mountView(Files);
    await flushPromises();

    const back = wrapper
      .findAllComponents(RouterLinkStub)
      .find((l: any) => l.text().includes("Back to my files"));
    expect(back, "the error should carry its own way out").toBeTruthy();
    expect(back!.props("to")).toBe("/files/");

    wrapper.unmount();
  });

  it("keeps retry but drops the controls that need a listing", async () => {
    const wrapper = mountView(Files);
    await flushPromises();

    const titles = wrapper
      .findAll("button")
      .map((b: any) => b.attributes("title") ?? b.attributes("aria-label"));

    expect(titles).toContain("Refresh");
    expect(titles).not.toContain("Switch view");
    expect(titles).not.toContain("Select multiple");

    wrapper.unmount();
  });

  // The store still holds the directory that loaded last — Files.vue never
  // clears it on failure — so anything acting on a directory has to stay off
  // this page rather than act on one the user has left.
  it("shows neither the sidebar nor the action rail", async () => {
    const wrapper = mountView(Files);
    await flushPromises();

    expect(wrapper.findComponent({ name: "FileActions" }).exists()).toBe(false);
    expect(wrapper.findComponent({ name: "StorageCard" }).exists()).toBe(false);
    expect(wrapper.findComponent({ name: "TwoColumns" }).exists()).toBe(false);

    wrapper.unmount();
  });

  it("still renders the listing normally when the fetch succeeds", async () => {
    mockFilesFetch.mockReset();
    mockFilesFetch.mockResolvedValue({
      path: "/case",
      name: "case",
      isDir: true,
      type: "",
      items: [],
      size: 0,
      modified: new Date().toISOString(),
    });

    const wrapper = mountView(Files);
    await flushPromises();

    expect(wrapper.text()).not.toContain("This location can't be reached.");
    expect(wrapper.findComponent({ name: "TwoColumns" }).exists()).toBe(true);

    wrapper.unmount();
  });
});

describe("an unreachable location inside a share", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockPubFetch.mockReset();
    mockPubFetch.mockRejectedValue(new StatusError("not found", 404));
  });

  it("sends a visitor back to the share root when a path inside it fails", async () => {
    shareParams.value = ["ABC123", "deep", "missing"];
    routePath.value = "/share/ABC123/deep/missing";

    const wrapper = mountView(Share);
    await flushPromises();

    const back = wrapper
      .findAllComponents(RouterLinkStub)
      .find((l: any) => l.text().includes("Back to the shared folder"));
    expect(back, "the share root is still a real place").toBeTruthy();
    expect(back!.props("to")).toBe("/share/ABC123/");

    wrapper.unmount();
  });

  // A visitor following a share link need not have an account, so /files would
  // answer with a login form rather than a way back.
  it("offers nothing — and never /files — when the share itself is gone", async () => {
    shareParams.value = ["GONE"];
    routePath.value = "/share/GONE";

    const wrapper = mountView(Share);
    await flushPromises();

    expect(wrapper.text()).not.toContain("Back to the shared folder");
    expect(linkTargets(wrapper)).not.toContain("/files");

    wrapper.unmount();
  });

  // Revoked, expired and never-issued all answer 404 — an expired link is
  // deleted the moment it is asked for — so one wording has to cover them
  // without claiming to know which, and without the generic file-browser
  // phrasing that reads as though the share is merely unreachable right now.
  it("names the share as gone rather than the location as unreachable", async () => {
    shareParams.value = ["GONE"];
    routePath.value = "/share/GONE";

    const wrapper = mountView(Share);
    await flushPromises();

    expect(wrapper.text()).toContain(
      "This share doesn't exist or has expired."
    );
    expect(wrapper.text()).not.toContain("This location can't be reached.");

    wrapper.unmount();
  });

  // The share is fine here; only the path inside it is not. Saying the share
  // has expired would send the visitor away from something that still works.
  it("keeps the generic wording for a bad path inside a live share", async () => {
    shareParams.value = ["ABC123", "deep", "missing"];
    routePath.value = "/share/ABC123/deep/missing";

    const wrapper = mountView(Share);
    await flushPromises();

    expect(wrapper.text()).toContain("This location can't be reached.");
    expect(wrapper.text()).not.toContain("This share doesn't exist");

    wrapper.unmount();
  });

  // The limiter is keyed per address and share, so this is reached by mistyping
  // a password, not only by attacking one. Falling through to the 500 wording
  // told that visitor the server had broken.
  it("says to wait when the password attempts are rate limited", async () => {
    shareParams.value = ["ABC123"];
    routePath.value = "/share/ABC123";
    mockPubFetch.mockReset();
    mockPubFetch.mockRejectedValue(new StatusError("too many", 429));

    const wrapper = mountView(Share);
    await flushPromises();

    expect(wrapper.text()).toContain(
      "Too many attempts. Try again in a minute."
    );
    expect(wrapper.text()).not.toContain("Something really went wrong.");

    wrapper.unmount();
  });

  it("still asks for the password on a protected share", async () => {
    shareParams.value = ["ABC123"];
    routePath.value = "/share/ABC123";
    mockPubFetch.mockReset();
    mockPubFetch.mockRejectedValue(new StatusError("unauthorized", 401));

    const wrapper = mountView(Share);
    await flushPromises();

    expect(wrapper.find("input[type=password]").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("This location can't be reached.");

    wrapper.unmount();
  });
});
