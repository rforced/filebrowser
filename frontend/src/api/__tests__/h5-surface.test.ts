import { describe, expect, it, vi } from "vitest";

// parseSurface is pure, but reaching it pulls in the api's shared fetch
// helpers and so the auth store behind them. Same stubs as the other api
// tests, only so the module graph resolves.
vi.mock("@/utils/constants", () => ({
  baseURL: "/test",
  origin: "http://localhost",
  name: "Test",
  staticURL: "/static",
  version: "0.0.0",
  authMethod: "password",
  theme: "light",
  tusSettings: { retryCount: 5, chunkSize: 10485760 },
  tusEndpoint: "/api/tus",
  domain: "",
  teamId: "",
  filesystemId: "",
}));

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" } } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

vi.mock("@/utils/auth", () => ({
  renew: vi.fn(),
  logout: vi.fn(),
}));

import { parseSurface, type H5SurfaceHeader } from "@/api/h5";

// Mirrors what http/h5_surface.go writes: an 8-byte magic, the header length,
// the JSON header padded to a 4-byte boundary, then the raw arrays. The
// padding is what lets the parser lay typed-array views over the buffer
// instead of copying it, so the encoder here pads exactly as the server does.
function encode(
  header: Partial<H5SurfaceHeader>,
  positions: number[],
  indices: number[],
  values?: number[],
  magic = "FBSURF01"
): ArrayBuffer {
  const full: H5SurfaceHeader = {
    stream: "STREAM_00",
    vertices: positions.length / 3,
    triangles: indices.length / 3,
    faces: 0,
    facesTotal: 0,
    stride: 1,
    bounds: [0, 0, 0, 1, 1, 1],
    range: [0, 0],
    boundaries: [],
    ...header,
  };

  let meta = new TextEncoder().encode(JSON.stringify(full));
  while (meta.length % 4 !== 0) {
    const padded = new Uint8Array(meta.length + 1);
    padded.set(meta);
    padded[meta.length] = 0x20; // space
    meta = padded;
  }

  const size =
    12 +
    meta.length +
    positions.length * 4 +
    indices.length * 4 +
    (values?.length ?? 0) * 4;
  const buffer = new ArrayBuffer(size);
  const bytes = new Uint8Array(buffer);
  const view = new DataView(buffer);

  bytes.set(new TextEncoder().encode(magic), 0);
  view.setUint32(8, meta.length, true);
  bytes.set(meta, 12);

  let offset = 12 + meta.length;
  for (const v of positions) {
    view.setFloat32(offset, v, true);
    offset += 4;
  }
  for (const v of indices) {
    view.setUint32(offset, v, true);
    offset += 4;
  }
  for (const v of values ?? []) {
    view.setFloat32(offset, v, true);
    offset += 4;
  }
  return buffer;
}

describe("parseSurface", () => {
  it("reads the header and lays views over the arrays", () => {
    const buffer = encode(
      {
        faces: 2,
        facesTotal: 2,
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
      },
      [0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0],
      [0, 1, 2, 1, 3, 2]
    );

    const surface = parseSurface(buffer);

    expect(surface.stream).toBe("STREAM_00");
    expect(surface.vertices).toBe(4);
    expect(surface.triangles).toBe(2);
    expect(surface.boundaries[0].name).toBe("PISTON");

    expect(Array.from(surface.positions)).toEqual([
      0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0,
    ]);
    expect(Array.from(surface.indices)).toEqual([0, 1, 2, 1, 3, 2]);
    // No scalar was requested, so no values were sent.
    expect(surface.values).toBeUndefined();
  });

  it("reads per-vertex values when a scalar was requested", () => {
    const buffer = encode(
      { scalar: "BOUND_HTC", range: [10, 30] },
      [0, 0, 0, 1, 0, 0, 0, 1, 0],
      [0, 1, 2],
      [10, 20, 30]
    );

    const surface = parseSurface(buffer);

    expect(surface.scalar).toBe("BOUND_HTC");
    expect(Array.from(surface.values!)).toEqual([10, 20, 30]);
    expect(surface.range).toEqual([10, 30]);
  });

  // A vertex touched only by faces with no usable reading arrives as NaN. It
  // has to stay NaN rather than become 0, which the ramp would draw as a
  // reading at the cold end of the legend.
  it("keeps NaN values distinguishable from zero", () => {
    const buffer = encode(
      { scalar: "TEMP", range: [0, 1] },
      [0, 0, 0, 1, 0, 0, 0, 1, 0],
      [0, 1, 2],
      [0, NaN, 1]
    );

    const surface = parseSurface(buffer);

    expect(surface.values![0]).toBe(0);
    expect(Number.isNaN(surface.values![1])).toBe(true);
    expect(surface.values![2]).toBe(1);
  });

  it("survives a header whose length is not already 4-byte aligned", () => {
    // A boundary name of an awkward length pushes the JSON off alignment; the
    // views would be unconstructable if the padding were dropped.
    const buffer = encode(
      {
        boundaries: [
          {
            id: 7,
            name: "ODD",
            faces: 1,
            triangles: 1,
            indexOffset: 0,
            indexCount: 3,
          },
        ],
      },
      [0, 0, 0, 2, 0, 0, 0, 2, 0],
      [0, 1, 2]
    );

    const surface = parseSurface(buffer);
    expect(Array.from(surface.positions)).toEqual([0, 0, 0, 2, 0, 0, 0, 2, 0]);
    expect(surface.boundaries[0].name).toBe("ODD");
  });

  it("refuses a response that is not a surface", () => {
    const buffer = encode({}, [0, 0, 0], [], undefined, "NOTASURF");
    expect(() => parseSurface(buffer)).toThrow(/not a surface/);
  });

  it("refuses a truncated response", () => {
    expect(() => parseSurface(new ArrayBuffer(4))).toThrow(/truncated/);

    const short = encode({}, [0, 0, 0, 1, 0, 0, 0, 1, 0], [0, 1, 2]);
    expect(() => parseSurface(short.slice(0, 14))).toThrow(/truncated/);
  });
});
