import { describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/files/photos/2026/" }),
}));

import Breadcrumbs from "../Breadcrumbs.vue";

const mountCrumbs = (slots: Record<string, string> = {}) =>
  mount(Breadcrumbs, {
    props: { base: "/files" },
    slots,
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: "en",
          missingWarn: false,
          fallbackWarn: false,
          messages: { en: {} },
        }),
      ],
      stubs: { RouterLink: { template: "<a><slot /></a>" } },
      directives: { tooltip: {} },
    },
  });

describe("Breadcrumbs actions slot", () => {
  it("renders the actions inside the breadcrumb row, after the crumbs", () => {
    const wrapper = mountCrumbs({
      actions: '<button id="switch-view">switch</button>',
    });

    const nav = wrapper.get("nav");
    const button = nav.get("#switch-view");

    expect(button.element.parentElement?.className).toContain("ms-auto");
    expect(nav.text()).toContain("2026");
  });

  it("adds no wrapper when no actions are passed", () => {
    expect(mountCrumbs().find(".ms-auto").exists()).toBe(false);
  });
});
