import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
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

function mountChrome(component: unknown, { loggedIn = false } = {}) {
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
    global: {
      plugins: [i18n, pinia],
      stubs: { Search: true, ThemeSwitch: true, RouterLink: true },
      directives: { tooltip: {} },
    },
  });
}

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

  it("shows the brand link standalone and drops it when embedded", () => {
    const brand = '[aria-label="Test Manager"]';

    expect(mountChrome(AppHeader).find(brand).exists()).toBe(true);

    state.embedded = true;
    const embedded = mountChrome(AppHeader);
    expect(embedded.find(brand).exists()).toBe(false);
    expect(embedded.find("header").exists()).toBe(true);
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
});
