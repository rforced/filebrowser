import { beforeEach, describe, expect, it, vi } from "vitest";

const surfaceMock = vi.hoisted(() => vi.fn());
vi.mock("@/api/h5", () => ({ surface: surfaceMock }));

import {
  clearSurfaceCache,
  fetchSurface,
  SURFACE_TRIANGLE_LIMIT,
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
        limit: SURFACE_TRIANGLE_LIMIT,
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
    // out by the entry cap before any of them resolve.
    const fifteenMB = 15 << 20;
    const pending = [];
    for (let i = 0; i < 10; i++) {
      pending.push(fetchSurface(`/case/post${i}.h5`, { stream: "STREAM_00" }));
    }
    resolvers.forEach((resolve) => resolve(fakeSized(fifteenMB)));
    await Promise.all(pending);

    surfaceMock.mockClear();
    for (let i = 2; i < 10; i++) {
      await fetchSurface(`/case/post${i}.h5`, { stream: "STREAM_00" });
    }
    expect(surfaceMock).not.toHaveBeenCalled();
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
