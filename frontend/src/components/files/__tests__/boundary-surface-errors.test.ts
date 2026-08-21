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

function mountSurface(props: Record<string, any> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: {
      en: {
        h5View: {
          meshTooLarge:
            "This mesh is too large for the viewer to read. No detail step will change that.",
          scalarTooLarge:
            "This mesh is too large to colour by a field. Switch to By boundary to see the geometry.",
          surfaceTooLarge:
            "This surface is too large to draw, even at the lowest detail.",
          surfaceTooLargeForStep:
            "This surface is too large to draw at this detail. Try a lower step.",
        },
      },
    },
  });
  return mount(BoundarySurface, {
    props: { path: "/case/post000001.h5", stream: "STREAM_00", ...props },
    global: { plugins: [i18n], directives: { tooltip: {} } },
  });
}

describe("BoundarySurface errors", () => {
  beforeEach(() => {
    clearSurfaceCache();
    mockSurface.mockReset();
  });

  // The server sends no body with a 413, so the raw failure reads as the bare
  // status line. A surface the extractor refuses is a fact about the file, not
  // a transport error, and the viewer says so.
  //
  // Above the lowest step the refusal is one a lower step may well answer, so
  // it says which lever there is; at the lowest there is nothing to suggest.
  it("explains a 413 and points at the detail step", async () => {
    const err: any = new Error("413 Request Entity Too Large");
    err.status = 413;
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface({ resolution: "ultra" });
    await flushPromises();

    expect(wrapper.text()).toContain("Try a lower step.");
    expect(wrapper.text()).not.toContain("413");

    wrapper.unmount();
  });

  it("stops suggesting a lower step once there is none", async () => {
    const err: any = new Error("413 Request Entity Too Large");
    err.status = 413;
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface({ resolution: "low" });
    await flushPromises();

    expect(wrapper.text()).toContain("even at the lowest detail");
    expect(wrapper.text()).not.toContain("Try a lower step.");
    expect(wrapper.text()).not.toContain("413");

    wrapper.unmount();
  });

  // The refusal that prompted all of this: a 27.4M-cell file whose 84M faces
  // the reader will not hold was answered with "try a lower step", so the user
  // walked the menu down to the bottom and got told the same thing again. No
  // step reduces the mesh — only the code separates that from a surface drawn
  // at too much detail.
  it("does not offer a detail step for a mesh it cannot read", async () => {
    const err: any = new Error("mesh too large to extract: 84125742 faces");
    err.status = 413;
    err.code = "meshTooLarge";
    err.params = { faces: "84125742", limit: "33554432" };
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface({ resolution: "ultra" });
    await flushPromises();

    expect(wrapper.text()).toContain("too large for the viewer to read");
    expect(wrapper.text()).not.toContain("Try a lower step.");

    wrapper.unmount();
  });

  // Colouring a CGNS wall inverts the cell section, which the geometry never
  // reads. Dropping the field is the lever, not dropping detail.
  it("points at the colour-by when only the scalar is out of reach", async () => {
    const err: any = new Error("too many face references to invert");
    err.status = 413;
    err.code = "scalarTooLarge";
    mockSurface.mockRejectedValue(err);

    const wrapper = mountSurface({
      resolution: "ultra",
      scalar: "TEMPERATURE",
    });
    await flushPromises();

    expect(wrapper.text()).toContain("Switch to By boundary");
    expect(wrapper.text()).not.toContain("Try a lower step.");

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
