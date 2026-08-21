import { beforeEach, describe, expect, it, vi } from "vitest";

const surfaceMock = vi.hoisted(() => vi.fn());
vi.mock("@/api/h5", () => ({ surface: surfaceMock }));

import {
  abortPendingSurfaces,
  clearSurfaceCache,
  DEFAULT_SURFACE_RESOLUTION,
  fetchSurface,
  PLAYBACK_SURFACE_RESOLUTION,
  SURFACE_TRIANGLE_LIMITS,
} from "../surfaceCache";

// The cache only reads positions.buffer.byteLength off a response.
const fakeSurface = (bytes = 64) => ({
  positions: new Float32Array(new ArrayBuffer(bytes)),
});

// Same shape, without allocating the megabytes it claims to carry.
const fakeSized = (bytes: number) => ({
  positions: { buffer: { byteLength: bytes } },
});

beforeEach(() => {
  clearSurfaceCache();
  surfaceMock.mockReset();
  surfaceMock.mockImplementation(async () => fakeSurface());
});

describe("fetchSurface", () => {
  it("shares one request between concurrent callers of the same frame", async () => {
    const req = { stream: "STREAM_00" };
    const [a, b] = await Promise.all([
      fetchSurface("/case/post1.h5", req),
      fetchSurface("/case/post1.h5", req),
    ]);
    expect(a).toBe(b);
    expect(surfaceMock).toHaveBeenCalledTimes(1);
    expect(surfaceMock).toHaveBeenCalledWith(
      "/case/post1.h5",
      {
        stream: "STREAM_00",
        scalar: undefined,
        limit: SURFACE_TRIANGLE_LIMITS[DEFAULT_SURFACE_RESOLUTION],
        edges: undefined,
      },
      expect.any(AbortSignal)
    );
  });

  it("keys on the request, not just the path", async () => {
    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    await fetchSurface("/case/post1.h5", {
      stream: "STREAM_00",
      scalar: "TEMPERATURE",
    });
    await fetchSurface("/case/post1.h5", { stream: "STREAM_00", edges: true });
    expect(surfaceMock).toHaveBeenCalledTimes(3);
  });

  it("keys on the resolution, so raising detail is not answered from the strided copy", async () => {
    await fetchSurface("/case/post1.h5", {
      stream: "STREAM_00",
      resolution: "low",
    });
    await fetchSurface("/case/post1.h5", {
      stream: "STREAM_00",
      resolution: "high",
    });
    expect(surfaceMock).toHaveBeenCalledTimes(2);
    expect(surfaceMock.mock.calls[0][1].limit).toBe(
      SURFACE_TRIANGLE_LIMITS.low
    );
    expect(surfaceMock.mock.calls[1][1].limit).toBe(
      SURFACE_TRIANGLE_LIMITS.high
    );
  });

  it("treats an unset resolution as the default rather than a key of its own", async () => {
    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    await fetchSurface("/case/post1.h5", {
      stream: "STREAM_00",
      resolution: DEFAULT_SURFACE_RESOLUTION,
    });
    expect(surfaceMock).toHaveBeenCalledTimes(1);
  });

  it("asks for fewer triangles at each lower step", () => {
    expect(SURFACE_TRIANGLE_LIMITS.low).toBeLessThan(
      SURFACE_TRIANGLE_LIMITS.medium
    );
    expect(SURFACE_TRIANGLE_LIMITS.medium).toBeLessThan(
      SURFACE_TRIANGLE_LIMITS.high
    );
    expect(SURFACE_TRIANGLE_LIMITS.high).toBeLessThan(
      SURFACE_TRIANGLE_LIMITS.ultra
    );
  });

  // A frame settles at full detail and playback buys frames with it; if these
  // ever met, playing would cost what pausing costs.
  it("plays back below the resolution a settled frame lands on", () => {
    expect(SURFACE_TRIANGLE_LIMITS[PLAYBACK_SURFACE_RESOLUTION]).toBeLessThan(
      SURFACE_TRIANGLE_LIMITS[DEFAULT_SURFACE_RESOLUTION]
    );
  });

  // The budgets are per-frame, and a still is not a frame. Defaulting to any
  // of them holes a surface that would have fitted whole — a 27.4M-cell case
  // measured 2,640,214 boundary triangles, which the 2M step strides by 2.
  it("names no triangle budget for a still, so nothing strides it by default", () => {
    const budgets = Object.values(SURFACE_TRIANGLE_LIMITS);
    expect(SURFACE_TRIANGLE_LIMITS[DEFAULT_SURFACE_RESOLUTION]).toBe(
      Math.max(...budgets)
    );
    expect(SURFACE_TRIANGLE_LIMITS[DEFAULT_SURFACE_RESOLUTION]).toBe(Infinity);
  });

  it("evicts the oldest frames once past the entry cap", async () => {
    for (let i = 0; i < 9; i++) {
      await fetchSurface(`/case/post${i}.h5`, { stream: "STREAM_00" });
    }
    surfaceMock.mockClear();

    // The newest is still cached; the oldest fell out and refetches.
    await fetchSurface("/case/post8.h5", { stream: "STREAM_00" });
    expect(surfaceMock).not.toHaveBeenCalled();
    await fetchSurface("/case/post0.h5", { stream: "STREAM_00" });
    expect(surfaceMock).toHaveBeenCalledTimes(1);
  });

  it("aborts what is still in flight when the cache is cleared", async () => {
    let signal: AbortSignal | undefined;
    surfaceMock.mockImplementation(
      (_path: string, _opts: unknown, s: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal = s;
          s.addEventListener("abort", () => {
            const err = new Error("aborted");
            err.name = "AbortError";
            reject(err);
          });
        })
    );

    const pending = fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    expect(signal?.aborted).toBe(false);

    clearSurfaceCache();
    expect(signal?.aborted).toBe(true);
    await expect(pending).rejects.toThrow("aborted");
  });

  it("does not strand the bytes of a frame evicted while still in flight", async () => {
    const resolvers: Array<(v: unknown) => void> = [];
    surfaceMock.mockImplementation(
      () => new Promise((resolve) => resolvers.push(resolve))
    );

    // Eight of these fit the byte budget; ten do not. The first two are pushed
    // out by the entry cap before any of them resolve — and if their bytes
    // were still charged afterwards the budget would evict a ninth that is
    // genuinely there. Sized against the budget deliberately: make these small
    // enough that ten of them fit and the stranding goes unnoticed.
    const thirtyMB = 30 << 20;
    const pending = [];
    for (let i = 0; i < 10; i++) {
      pending.push(fetchSurface(`/case/post${i}.h5`, { stream: "STREAM_00" }));
    }
    resolvers.forEach((resolve) => resolve(fakeSized(thirtyMB)));
    await Promise.all(pending);

    surfaceMock.mockClear();
    for (let i = 2; i < 10; i++) {
      await fetchSurface(`/case/post${i}.h5`, { stream: "STREAM_00" });
    }
    expect(surfaceMock).not.toHaveBeenCalled();
  });

  // What suspending playback costs the box: the frame being cut is dropped,
  // the frames already paid for are still there when the user comes back.
  it("drops what is in flight and keeps what has landed", async () => {
    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });

    let signal: AbortSignal | undefined;
    surfaceMock.mockImplementation(
      (_path: string, _opts: unknown, s: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal = s;
          s.addEventListener("abort", () => {
            const err = new Error("aborted");
            err.name = "AbortError";
            reject(err);
          });
        })
    );
    const inFlight = fetchSurface("/case/post2.h5", { stream: "STREAM_00" });

    abortPendingSurfaces();
    expect(signal?.aborted).toBe(true);
    await expect(inFlight).rejects.toThrow("aborted");

    surfaceMock.mockClear();
    surfaceMock.mockImplementation(async () => fakeSurface());
    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    expect(surfaceMock).not.toHaveBeenCalled();

    // The aborted one left nothing behind that would replay the failure.
    await fetchSurface("/case/post2.h5", { stream: "STREAM_00" });
    expect(surfaceMock).toHaveBeenCalledTimes(1);
  });

  it("does not keep a failed fetch, so retrying reaches the network", async () => {
    surfaceMock.mockRejectedValueOnce(new Error("boom"));
    await expect(
      fetchSurface("/case/post1.h5", { stream: "STREAM_00" })
    ).rejects.toThrow("boom");

    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    expect(surfaceMock).toHaveBeenCalledTimes(2);
  });
});

// Stills stopped being strided, so they got larger: the largest case measured
// is 45MB drawn whole and 66MB with edges. The budget has to hold enough of
// those that comparing two fields, or turning edges on and back off, is not a
// refetch each way.
describe("surface cache budget against measured payloads", () => {
  const ULTRA_BYTES = 45 << 20;
  const ULTRA_EDGES_BYTES = 66 << 20;
  const PLAYBACK_BYTES = 6.4 * (1 << 20);

  it("keeps several full-fidelity stills, not just the one on screen", async () => {
    surfaceMock.mockImplementation(async () => fakeSized(ULTRA_BYTES));

    const fields = ["TEMPERATURE", "PRESSURE", "VELOCITY_X"];
    for (const scalar of fields) {
      await fetchSurface("/case/post1.h5", { stream: "STREAM_00", scalar });
    }
    surfaceMock.mockClear();

    // Going back to the first field must not pay for it again.
    for (const scalar of fields) {
      await fetchSurface("/case/post1.h5", { stream: "STREAM_00", scalar });
    }
    expect(surfaceMock).not.toHaveBeenCalled();
  });

  it("holds a still and its edged twin together", async () => {
    surfaceMock.mockImplementation(async (_p: string, opts: any) =>
      fakeSized(opts.edges ? ULTRA_EDGES_BYTES : ULTRA_BYTES)
    );

    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    await fetchSurface("/case/post1.h5", { stream: "STREAM_00", edges: true });
    surfaceMock.mockClear();

    await fetchSurface("/case/post1.h5", { stream: "STREAM_00" });
    expect(surfaceMock).not.toHaveBeenCalled();
  });

  // Playback runs at the low step, where the entry cap binds long before the
  // byte cap does. If the bytes ever became the limit there, a scrub back
  // would refetch every frame it passed.
  it("lets playback fill the entry cap without hitting the byte cap", async () => {
    surfaceMock.mockImplementation(async () => fakeSized(PLAYBACK_BYTES));

    for (let i = 0; i < 8; i++) {
      await fetchSurface(`/case/post${i}.h5`, {
        stream: "STREAM_00",
        resolution: "low",
      });
    }
    surfaceMock.mockClear();

    for (let i = 0; i < 8; i++) {
      await fetchSurface(`/case/post${i}.h5`, {
        stream: "STREAM_00",
        resolution: "low",
      });
    }
    expect(surfaceMock).not.toHaveBeenCalled();
  });

  // A surface that fills the budget on its own is still the one being looked
  // at; dropping it would refetch on every redraw.
  it("keeps a single entry that alone exceeds the budget", async () => {
    surfaceMock.mockImplementation(async () => fakeSized(512 << 20));

    await fetchSurface("/case/huge.h5", { stream: "STREAM_00" });
    surfaceMock.mockClear();

    await fetchSurface("/case/huge.h5", { stream: "STREAM_00" });
    expect(surfaceMock).not.toHaveBeenCalled();
  });
});
