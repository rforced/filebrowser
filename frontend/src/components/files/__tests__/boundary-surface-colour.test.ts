import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createI18n } from "vue-i18n";

// three needs a WebGL context happy-dom cannot give, so the renderer and the
// scene graph are stubbed down to what build() actually touches: attributes,
// materials and parentage. Everything colour-related stays the real code.
vi.mock("three", () => {
  const built: any[] = ((globalThis as any).__built = []);
  const geos: any[] = ((globalThis as any).__geos = []);
  class Attr {
    needsUpdate = false;
    constructor(
      public array: any,
      public itemSize: number
    ) {}
  }
  class Geo {
    attributes: Record<string, any> = {};
    index: any = null;
    constructor() {
      geos.push(this);
    }
    setAttribute(name: string, attr: any) {
      this.attributes[name] = attr;
    }
    setIndex(i: any) {
      this.index = i;
    }
    dispose() {}
  }
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
    disposed = false;
    constructor(opts: any = {}) {
      Object.assign(this, opts);
    }
    dispose() {
      this.disposed = true;
    }
  }
  class StandardMat extends Mat {
    constructor(opts: any = {}) {
      super(opts);
      built.push(this);
    }
  }
  return {
    BufferAttribute: Attr,
    Float32BufferAttribute: Attr,
    BufferGeometry: Geo,
    Group: class extends Obj3D {},
    Mesh: class extends Obj3D {
      constructor(
        public geometry: any,
        public material: any
      ) {
        super();
      }
    },
    LineSegments: class extends Obj3D {
      isLineSegments = true;
      constructor(
        public geometry: any,
        public material: any
      ) {
        super();
      }
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
    MeshStandardMaterial: StandardMat,
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
import { NO_VALUE } from "@/utils/colorRamp";
import BoundarySurface from "../BoundarySurface.vue";

// Two triangles over four vertices, one boundary — the smallest surface that
// still exercises the shared position/colour attributes.
const makeSurface = (scalar: string | undefined, values: number[]) => ({
  stream: "STREAM_00",
  vertices: 4,
  triangles: 2,
  faces: 2,
  facesTotal: 2,
  stride: 1,
  bounds: [0, 0, 0, 1, 1, 1],
  scalar,
  range: scalar ? [Math.min(...values), Math.max(...values)] : [0, 0],
  boundaries: [
    {
      id: 1,
      name: "PISTON",
      faces: 2,
      triangles: 2,
      indexOffset: 0,
      indexCount: 6,
    },
  ],
  positions: new Float32Array([0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0]),
  indices: new Uint32Array([0, 1, 2, 0, 2, 3]),
  values: scalar ? new Float32Array(values) : undefined,
});

function mountSurface(props: Record<string, any> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    missingWarn: false,
    fallbackWarn: false,
    messages: { en: {} },
  });
  return mount(BoundarySurface, {
    props: {
      path: "/case/post000001_+1.00000e+00.h5",
      stream: "STREAM_00",
      ...props,
    },
    global: { plugins: [i18n], directives: { tooltip: {} } },
  });
}

const greyCount = (rgb: Float32Array) => {
  let n = 0;
  for (let i = 0; i < rgb.length / 3; i++) {
    if (
      Math.abs(rgb[i * 3] - NO_VALUE[0]) < 1e-6 &&
      Math.abs(rgb[i * 3 + 1] - NO_VALUE[1]) < 1e-6 &&
      Math.abs(rgb[i * 3 + 2] - NO_VALUE[2]) < 1e-6
    ) {
      n++;
    }
  }
  return n;
};

const built = () => (globalThis as any).__built as any[];
const geos = () => (globalThis as any).__geos as any[];

// The colour attribute of the geometry built last, which is what the user is
// looking at. Null when the surface was drawn per boundary instead.
const currentColours = (): Float32Array | null => {
  const all = geos();
  const last = all[all.length - 1];
  return last?.attributes?.color?.array ?? null;
};

describe("BoundarySurface colouring", () => {
  beforeEach(() => {
    clearSurfaceCache();
    mockSurface.mockReset();
    built().length = 0;
    geos().length = 0;
  });

  it("keeps vertex colours when the scalar and the detail step change back to back", async () => {
    mockSurface.mockImplementation((_path: string, opts: any) =>
      Promise.resolve(
        makeSurface(opts.scalar, opts.scalar ? [300, 500, 700, 900] : [])
      )
    );

    const wrapper = mountSurface({ scalar: undefined });
    await flushPromises();

    await wrapper.setProps({ scalar: "TEMPERATURE" });
    await wrapper.setProps({ resolution: "ultra" });
    await flushPromises();

    const all = built();
    const last = all[all.length - 1];
    expect(last.vertexColors).toBe(true);

    const colours = currentColours();
    expect(colours).not.toBeNull();
    expect(greyCount(colours as Float32Array)).toBe(0);

    wrapper.unmount();
  });

  // Changing "colour by" and then "Detail" leaves two requests in flight. The
  // second is the one the controls now describe, so its surface must be the one
  // on screen however the two responses race.
  it("draws the newest request when the responses land out of order", async () => {
    const pending: { opts: any; resolve: (v: any) => void }[] = [];
    mockSurface.mockImplementation(
      (_path: string, opts: any) =>
        new Promise((resolve) => pending.push({ opts, resolve }))
    );

    const wrapper = mountSurface({ scalar: "TEMPERATURE" });
    await flushPromises();
    pending.shift()?.resolve(makeSurface("TEMPERATURE", [300, 500, 700, 900]));
    await flushPromises();

    await wrapper.setProps({ scalar: "PRESSURE" });
    await wrapper.setProps({ resolution: "ultra" });
    await flushPromises();

    expect(pending).toHaveLength(2);

    // The newest lands first, then the superseded one — the order a cache hit
    // on the older key produces in the browser.
    const [older, newer] = pending;
    newer.resolve(makeSurface("PRESSURE", [1e5, 2e5, 3e5, 4e5]));
    await flushPromises();
    older.resolve(makeSurface("PRESSURE", [1e5, 2e5, 3e5, 4e5]));
    await flushPromises();

    const colours = currentColours();
    expect(colours).not.toBeNull();
    expect(greyCount(colours as Float32Array)).toBe(0);

    const all = built();
    expect(all[all.length - 1].vertexColors).toBe(true);

    wrapper.unmount();
  });

  // A surface fetched without a scalar carries no values, so switching to
  // "colour by boundary" and back must not leave the mesh on the neutral grey
  // the ramp uses for a reading it does not have.
  it("recovers vertex colours after a pass through colour-by-boundary", async () => {
    mockSurface.mockImplementation((_path: string, opts: any) =>
      Promise.resolve(
        makeSurface(opts.scalar, opts.scalar ? [300, 500, 700, 900] : [])
      )
    );

    const wrapper = mountSurface({ scalar: "TEMPERATURE" });
    await flushPromises();

    await wrapper.setProps({ scalar: undefined });
    await wrapper.setProps({ resolution: "low" });
    await flushPromises();
    expect(currentColours()).toBeNull();

    await wrapper.setProps({ scalar: "TEMPERATURE" });
    await wrapper.setProps({ resolution: "ultra" });
    await flushPromises();

    const colours = currentColours();
    expect(colours).not.toBeNull();
    expect(greyCount(colours as Float32Array)).toBe(0);

    wrapper.unmount();
  });

  // Pins the symptom to its cause: a uniformly pale grey wall is the ramp's
  // NO_VALUE, and the only way to paint every vertex with it is a values array
  // the client reads as non-finite throughout.
  it("paints the whole wall NO_VALUE grey when every value is non-finite", async () => {
    mockSurface.mockImplementation(() =>
      Promise.resolve({
        ...makeSurface("TEMPERATURE", [0, 0, 0, 0]),
        // What the server sends when no kept face resolved a cell value, and
        // what a float32 overflow of a large field sends for the same vertices.
        values: new Float32Array([NaN, NaN, Infinity, NaN]),
        range: [0, 0],
      })
    );

    const wrapper = mountSurface({ scalar: "TEMPERATURE" });
    await flushPromises();

    const colours = currentColours();
    expect(colours).not.toBeNull();
    expect(greyCount(colours as Float32Array)).toBe(4);

    wrapper.unmount();
  });

  it("survives a stale lock left over from the previous scalar", async () => {
    mockSurface.mockImplementation((_path: string, opts: any) =>
      Promise.resolve(makeSurface(opts.scalar, [1e5, 2e5, 3e5, 4e5]))
    );

    // The lock belongs to a temperature field; the surface arriving is a
    // pressure field, three orders of magnitude away.
    const wrapper = mountSurface({
      scalar: "PRESSURE",
      range: [300, 900],
    });
    await flushPromises();

    const colours = currentColours();
    expect(colours).not.toBeNull();
    expect(greyCount(colours as Float32Array)).toBe(0);

    wrapper.unmount();
  });
});
