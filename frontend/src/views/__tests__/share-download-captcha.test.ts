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
  recaptcha: "true",
  recaptchaKey: "site-key",
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
  cspNonce: "",
}));

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" } } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

const mockPubFetch = vi.fn();
const mockPubDownload = vi.fn();
vi.mock("@/api", () => ({
  files: {
    fetch: vi.fn(),
    getDownloadURL: () => "/download",
    usage: vi.fn().mockResolvedValue({ used: 0, total: 0 }),
  },
  pub: {
    fetch: (...a: any[]) => mockPubFetch(...a),
    getDownloadURL: () => "/download",
    download: (...a: any[]) => mockPubDownload(...a),
  },
  users: { update: vi.fn() },
}));

const mockExecute = vi.fn();
const mockMount = vi.fn();
const mockUnmount = vi.fn();
vi.mock("@/utils/recaptcha", () => ({
  recaptchaEnabled: true,
  mountRecaptcha: (...a: any[]) => mockMount(...a),
  unmountRecaptcha: (...a: any[]) => mockUnmount(...a),
  executeRecaptcha: (...a: any[]) => mockExecute(...a),
}));

const routePath = { value: "/share/ABC123" };
const shareParams = { value: ["ABC123"] as string[] };
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

import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { StatusError } from "@/api/utils";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import Share from "@/views/Share.vue";

dayjs.extend(relativeTime);

const modified = new Date().toISOString();

const sharedFolder = () => ({
  path: "/",
  name: "shared",
  isDir: true,
  type: "",
  size: 0,
  modified,
  items: [
    {
      index: 0,
      name: "a.txt",
      isDir: false,
      path: "/a.txt",
      url: "/share/ABC123/a.txt",
      type: "text",
      size: 1,
      modified,
    },
    {
      index: 1,
      name: "b.txt",
      isDir: false,
      path: "/b.txt",
      url: "/share/ABC123/b.txt",
      type: "text",
      size: 2,
      modified,
    },
  ],
});

function i18nPlugin() {
  return createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: {
      en: {
        buttons: { download: "Download", submit: "Submit" },
        files: { loading: "Loading", files: "Files" },
        download: { downloadFolder: "Download folder" },
        login: {
          password: "Password",
          wrongCredentials: "Wrong credentials",
          captchaFailed: "Security verification failed.",
        },
      },
    },
  });
}

const mountShare = () =>
  mount(Share, {
    global: {
      plugins: [i18nPlugin(), createPinia()],
      directives: { tooltip: {}, focus: {} },
      components: { RouterLink: RouterLinkStub },
      stubs: {
        RouterLink: RouterLinkStub,
        Item: true,
      },
      provide: { $showError: vi.fn(), $showSuccess: vi.fn() },
    },
  });

const downloadButton = (wrapper: any) =>
  wrapper.findAll("button").find((b: any) => b.text().includes("Download"));

beforeEach(() => {
  setActivePinia(createPinia());
  mockPubFetch.mockReset();
  mockPubDownload.mockReset();
  mockExecute.mockReset();
  mockMount.mockReset();
  mockUnmount.mockReset();
  routePath.value = "/share/ABC123";
  shareParams.value = ["ABC123"];
});

// The link in the sidebar could only ever hand out a zip, while the same
// download from a signed-in listing offers five formats.
describe("downloading a shared folder", () => {
  beforeEach(() => {
    mockPubFetch.mockResolvedValue(sharedFolder());
  });

  it("asks for a format instead of assuming zip", async () => {
    const wrapper = mountShare();
    await flushPromises();

    await downloadButton(wrapper)!.trigger("click");

    const layoutStore = useLayoutStore();
    expect(layoutStore.currentPromptName).toBe("download");

    layoutStore.currentPrompt!.confirm!("tarzst");
    expect(mockPubDownload).toHaveBeenCalledWith("tarzst", "ABC123", "", "/");

    wrapper.unmount();
  });

  it("takes the format for a selection too", async () => {
    const wrapper = mountShare();
    await flushPromises();

    const fileStore = useFileStore();
    fileStore.selected = [0, 1];
    await wrapper.vm.$nextTick();

    await downloadButton(wrapper)!.trigger("click");

    const layoutStore = useLayoutStore();
    layoutStore.currentPrompt!.confirm!("targz");
    expect(mockPubDownload).toHaveBeenCalledWith(
      "targz",
      "ABC123",
      "",
      "/a.txt",
      "/b.txt"
    );

    wrapper.unmount();
  });

  // A single file has nothing to archive, so it keeps the plain link.
  it("leaves a single shared file as a direct link", async () => {
    mockPubFetch.mockResolvedValue({
      path: "/report.pdf",
      name: "report.pdf",
      isDir: false,
      type: "pdf",
      size: 10,
      modified,
      items: [],
    });

    const wrapper = mountShare();
    await flushPromises();

    expect(downloadButton(wrapper)).toBeUndefined();
    expect(wrapper.find("a[href='/download']").exists()).toBe(true);

    wrapper.unmount();
  });
});

describe("the share password form", () => {
  const unauthorized = (code?: string) => {
    const err = new StatusError("unauthorized", 401);
    err.code = code;
    return err;
  };

  const submitPassword = async (wrapper: any, password: string) => {
    await wrapper.find("input[type=password]").setValue(password);
    await wrapper
      .findAll("button")
      .find((b: any) => b.text().includes("Submit"))!
      .trigger("click");
    await flushPromises();
  };

  it("proves the visitor is human before the password is checked", async () => {
    mockPubFetch.mockRejectedValue(unauthorized());
    mockExecute.mockResolvedValue("fresh-token");

    const wrapper = mountShare();
    await flushPromises();

    mockPubFetch.mockResolvedValue(sharedFolder());
    await submitPassword(wrapper, "hunter2");

    expect(mockExecute).toHaveBeenCalledWith("share");
    expect(mockPubFetch).toHaveBeenLastCalledWith(
      "/share/ABC123",
      "hunter2",
      "fresh-token"
    );

    wrapper.unmount();
  });

  // The server only demands a token once an address has guessed wrong, which
  // can happen mid-visit to someone who shares an address with a guesser.
  it("answers a captcha demanded mid-visit without sending the visitor back", async () => {
    mockExecute.mockResolvedValue("retry-token");
    mockPubFetch.mockRejectedValueOnce(unauthorized("captchaRequired"));
    mockPubFetch.mockResolvedValue(sharedFolder());

    const wrapper = mountShare();
    await flushPromises();

    expect(mockPubFetch).toHaveBeenCalledTimes(2);
    expect(mockPubFetch).toHaveBeenLastCalledWith(
      "/share/ABC123",
      "",
      "retry-token"
    );
    expect(wrapper.find("input[type=password]").exists()).toBe(false);

    wrapper.unmount();
  });

  // Google's script plants a badge in the corner of the page and keeps it
  // there. It belongs to the password card, not to the share behind it.
  it("takes the captcha off the page once the share is open", async () => {
    mockExecute.mockResolvedValue("fresh-token");
    mockPubFetch.mockRejectedValue(unauthorized());

    const wrapper = mountShare();
    await flushPromises();
    expect(mockMount).toHaveBeenCalled();
    expect(mockUnmount).not.toHaveBeenCalled();

    mockPubFetch.mockResolvedValue(sharedFolder());
    await submitPassword(wrapper, "hunter2");

    expect(mockUnmount).toHaveBeenCalled();

    wrapper.unmount();
    expect(mockUnmount).toHaveBeenCalledTimes(1);
  });

  it("never loads it for a share that opens without one", async () => {
    mockPubFetch.mockResolvedValue(sharedFolder());

    const wrapper = mountShare();
    await flushPromises();

    expect(mockMount).not.toHaveBeenCalled();

    wrapper.unmount();
  });

  it("says the verification failed rather than blaming the password", async () => {
    mockExecute.mockResolvedValue("stale-token");
    mockPubFetch.mockRejectedValue(unauthorized("captchaFailed"));

    const wrapper = mountShare();
    await flushPromises();
    await submitPassword(wrapper, "hunter2");

    expect(wrapper.text()).toContain("Security verification failed.");
    expect(wrapper.text()).not.toContain("Wrong credentials");

    wrapper.unmount();
  });

  it("still blames the password when that is what was wrong", async () => {
    mockExecute.mockResolvedValue("fresh-token");
    mockPubFetch.mockRejectedValue(unauthorized());

    const wrapper = mountShare();
    await flushPromises();

    // The locked share on arrival is not a failed attempt.
    expect(wrapper.text()).not.toContain("Wrong credentials");

    await submitPassword(wrapper, "wrong");
    expect(wrapper.text()).toContain("Wrong credentials");

    wrapper.unmount();
  });
});
