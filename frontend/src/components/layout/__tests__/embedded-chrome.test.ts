import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";

const state = vi.hoisted(() => ({ embedded: false }));
vi.mock("@/utils/embedded", () => ({
  get embedded() {
    return state.embedded;
  },
  framed: false,
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

import AppFooter from "../AppFooter.vue";
import AppHeader from "../AppHeader.vue";

function mountChrome(component: unknown) {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: { en: {} },
  });

  return mount(component as never, {
    global: {
      plugins: [i18n, createPinia()],
      stubs: { Search: true, ThemeSwitch: true, RouterLink: true },
      directives: { tooltip: {} },
    },
  });
}

describe("embedded chrome", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    state.embedded = false;
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
});
