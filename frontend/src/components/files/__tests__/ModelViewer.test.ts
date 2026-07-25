import { describe, expect, it } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createI18n } from "vue-i18n";

import ModelViewer from "../ModelViewer.vue";

function createI18nPlugin() {
  return createI18n({
    legacy: false,
    locale: "en",
    messages: {
      en: {
        buttons: {
          resetView: "Reset view",
          wireframe: "Toggle wireframe",
        },
        files: {
          modelLoadFailed: "Failed to load 3D model.",
          modelTooLarge: "3D model is too large for preview (>100MB).",
          modelNoWebgl: "3D preview is not available: WebGL is not supported.",
        },
      },
    },
  });
}

function mountViewer(
  props: Partial<{ src: string; extension: string; size: number }> = {}
) {
  return mount(ModelViewer, {
    props: {
      src: "/api/raw/model.stl?inline=true&auth=token",
      extension: ".stl",
      size: 1024,
      ...props,
    },
    global: {
      plugins: [createI18nPlugin()],
    },
  });
}

describe("ModelViewer", () => {
  // happy-dom provides no WebGL context, which stands in for browsers where
  // WebGL is unsupported or disabled.
  it("degrades to a message instead of throwing when WebGL is unavailable", async () => {
    const wrapper = mountViewer();
    await flushPromises();

    expect(wrapper.text()).toContain("WebGL is not supported");
    // The spinner must not be left running once initialisation has failed.
    expect(wrapper.find(".spinner").exists()).toBe(false);

    wrapper.unmount();
  });

  it("always renders a canvas so it is laid out before the first frame", () => {
    const wrapper = mountViewer();

    expect(wrapper.find("canvas.model-canvas").exists()).toBe(true);

    wrapper.unmount();
  });

  it("unmounts cleanly", async () => {
    const wrapper = mountViewer();
    await flushPromises();

    expect(() => wrapper.unmount()).not.toThrow();
  });
});
