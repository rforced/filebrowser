import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, RouterLinkStub } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";

const state = vi.hoisted(() => ({ embedded: false, path: "/files/" }));
vi.mock("@/utils/embedded", () => ({
  get embedded() {
    return state.embedded;
  },
  framed: false,
}));

const push = vi.hoisted(() => vi.fn());
vi.mock("vue-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => ({
    get path() {
      return state.path;
    },
  }),
  useRouter: () => ({ push }),
}));

vi.mock("@/utils/constants", () => ({
  baseURL: "/test",
  origin: "http://localhost",
  name: "Test Manager",
  staticURL: "/static",
  disableExternal: false,
  disableUsedPercentage: false,
  recaptcha: "",
  recaptchaKey: "",
  version: "0.0.0",
  authMethod: "hook",
  logoutPage: "/login",
  theme: "light",
  enableThumbs: false,
  resizePreview: false,
  tusSettings: { retryCount: 5, chunkSize: 10485760 },
  tusEndpoint: "/api/tus",
  logoURL: "/static/img/logo.svg",
  hideLoginButton: true,
  domain: "",
  teamId: "",
  filesystemId: "",
}));

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" } } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

import { useAuthStore } from "@/stores/auth";

import AppFooter from "../AppFooter.vue";
import AppHeader from "../AppHeader.vue";
import FileActions from "@/components/files/FileActions.vue";

function mountChrome(
  component: unknown,
  { loggedIn = false, props = {} } = {}
) {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: { en: {} },
  });

  const pinia = createPinia();
  setActivePinia(pinia);

  if (loggedIn) {
    useAuthStore().user = { username: "someone" } as IUser;
  }

  return mount(component as never, {
    props,
    global: {
      plugins: [i18n, pinia],
      stubs: { Search: true, ThemeSwitch: true, RouterLink: RouterLinkStub },
      directives: { tooltip: {} },
    },
  });
}

const settingsLink = (wrapper: ReturnType<typeof mountChrome>) =>
  wrapper
    .findAllComponents(RouterLinkStub)
    .filter((link) => link.props("to") === "/settings/profile");

describe("embedded chrome", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    state.embedded = false;
    state.path = "/files/";
  });

  it("shows the footer standalone and drops it when embedded", () => {
    expect(mountChrome(AppFooter).find("footer").exists()).toBe(true);

    state.embedded = true;
    expect(mountChrome(AppFooter).find("footer").exists()).toBe(false);
  });

  it("shows the header standalone and drops it whole when embedded", () => {
    const standalone = mountChrome(AppHeader);
    expect(standalone.find("header").exists()).toBe(true);
    expect(standalone.find('[aria-label="Test Manager"]').exists()).toBe(true);

    state.embedded = true;
    expect(mountChrome(AppHeader).find("header").exists()).toBe(false);
  });

  it("points the header action at settings, and back at the files once there", async () => {
    const onFiles = mountChrome(AppHeader, { loggedIn: true });

    expect(onFiles.find("i.fa-gear").exists()).toBe(true);
    await onFiles.get("i.fa-gear").trigger("click");
    expect(push).toHaveBeenCalledWith({ path: "/settings/profile" });

    state.path = "/settings/shares";
    const inSettings = mountChrome(AppHeader, { loggedIn: true });

    expect(inSettings.find("i.fa-gear").exists()).toBe(false);
    await inSettings.get("i.fa-folder-open").trigger("click");
    expect(push).toHaveBeenCalledWith({ path: "/files/" });
  });

  it("adds settings to the sidebar stack only when embedded", () => {
    expect(
      settingsLink(mountChrome(FileActions, { loggedIn: true })).length
    ).toBe(0);

    state.embedded = true;
    const embedded = settingsLink(mountChrome(FileActions, { loggedIn: true }));
    expect(embedded.length).toBe(1);
    expect(embedded[0].find("i.fa-gear").exists()).toBe(true);
  });

  it("adds settings to the mobile rail only when embedded", () => {
    const props = { variant: "rail" as const };

    expect(
      settingsLink(mountChrome(FileActions, { loggedIn: true, props })).length
    ).toBe(0);

    state.embedded = true;
    expect(
      settingsLink(mountChrome(FileActions, { loggedIn: true, props })).length
    ).toBe(1);
  });
});
