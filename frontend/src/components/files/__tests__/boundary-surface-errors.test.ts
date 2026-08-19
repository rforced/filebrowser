import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createI18n } from "vue-i18n";

vi.mock("three", () => {
  class Obj3D {
    children: any[] = [];
    userData: Record<string, any> = {};
    visible = true;
    position = { set() {} };
    add(child: any) {
      this.children.push(child);
    }
    remove(child: any) {
      this.children = this.children.filter((c) => c !== child);
    }
    traverse(fn: (o: any) => void) {
      fn(this);
      for (const c of this.children) c.traverse?.(fn);
    }
  }
  class Mat {
    constructor(opts: any = {}) {
      Object.assign(this, opts);
    }
    dispose() {}
  }
  return {
    BufferAttribute: class {
      constructor(
        public array: any,
        public itemSize: number
      ) {}
    },
    Float32BufferAttribute: class {
      constructor(
        public array: any,
        public itemSize: number
      ) {}
    },
    BufferGeometry: class {
      attributes: Record<string, any> = {};
      setAttribute() {}
      setIndex() {}
      dispose() {}
    },
    Group: class extends Obj3D {},
    Mesh: class extends Obj3D {},
    LineSegments: class extends Obj3D {
      isLineSegments = true;
    },
    Scene: class extends Obj3D {},
    Color: class {
      setHSL() {
        return this;
      }
    },
    DirectionalLight: class extends Obj3D {},
    HemisphereLight: class extends Obj3D {},
    DoubleSide: 2,
    LineBasicMaterial: Mat,
    MeshBasicMaterial: Mat,
    MeshStandardMaterial: Mat,
    PerspectiveCamera: class extends Obj3D {
      aspect = 1;
      near = 0.1;
      far = 100;
      updateProjectionMatrix() {}
    },
    WebGLRenderer: class {
      domElement = {};
      setPixelRatio() {}
      setSize() {}
      render() {}
      dispose() {}
      forceContextLoss() {}
    },
  };
});

vi.mock("three/addons/controls/OrbitControls.js", () => ({
  OrbitControls: class {
    target = { set() {} };
    enableDamping = false;
    dampingFactor = 0;
    addEventListener() {}
    update() {
      return false;
    }
    dispose() {}
  },
}));

const mockSurface = vi.fn();
vi.mock("@/api/h5", () => ({ surface: (...a: any[]) => mockSurface(...a) }));

import { clearSurfaceCache } from "@/utils/surfaceCache";
import BoundarySurface from "../BoundarySurface.vue";

function mountSurface() {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: {
      en: { h5View: { surfaceTooLarge: "This mesh is too large to draw." } },
    },
  });
  return mount(BoundarySurface, {
    props: { path: "/case/post000001.h5", stream: "STREAM_00" },
    global: { plugins: [i18n], directives: { tooltip: {} } },
  });
}

describe("BoundarySurface errors", () => {
  beforeEach(() => {
    clearSurfaceCache();
    mockSurface.mockReset();
  });

  // The server sends no body with a 413, so the raw failure reads as the bare
  // status line. A mesh the extractor refuses is a fact about the file, not a
  // transport error, and the viewer says so.
  it("explains a 413 instead of showing the bare status", async () => {
    const err: any = new Error("413 Request Entity Too Large");
    err.status = 413;
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface();
    await flushPromises();

    expect(wrapper.text()).toContain("This mesh is too large to draw.");
    expect(wrapper.text()).not.toContain("413");

    wrapper.unmount();
  });

  it("passes other failures through unchanged", async () => {
    const err: any = new Error("STREAM_00 carries no face connectivity");
    err.status = 404;
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface();
    await flushPromises();

    expect(wrapper.text()).toContain("carries no face connectivity");

    wrapper.unmount();
  });

  it("stays silent when the request was cancelled", async () => {
    const err: any = new Error("000 No connection");
    err.is_canceled = true;
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface();
    await flushPromises();

    expect(wrapper.text()).not.toContain("No connection");

    wrapper.unmount();
  });
});
