import { beforeEach, describe, expect, it, vi } from "vitest";

const parcelsMock = vi.hoisted(() => vi.fn());
vi.mock("@/api/h5", () => ({ parcels: parcelsMock }));

import { clearParcelCache, fetchParcels, PARCEL_LIMIT } from "../parcelCache";

const fakeCloud = () => ({ points: [0, 0, 0] });

beforeEach(() => {
  clearParcelCache();
  parcelsMock.mockReset();
  parcelsMock.mockImplementation(async () => fakeCloud());
});

describe("fetchParcels", () => {
  it("shares one request between concurrent callers of the same frame", async () => {
    const req = { group: "STREAM_00/PARCEL_DATA/LIQPARCEL_1" };
    const [a, b] = await Promise.all([
      fetchParcels("/case/post1.h5", req),
      fetchParcels("/case/post1.h5", req),
    ]);
    expect(a).toBe(b);
    expect(parcelsMock).toHaveBeenCalledTimes(1);
    expect(parcelsMock).toHaveBeenCalledWith(
      "/case/post1.h5",
      "STREAM_00/PARCEL_DATA/LIQPARCEL_1",
      { scalar: undefined, limit: PARCEL_LIMIT },
      expect.any(AbortSignal)
    );
  });

  it("keys on group and scalar, not just the path", async () => {
    await fetchParcels("/case/post1.h5", { group: "A" });
    await fetchParcels("/case/post1.h5", { group: "B" });
    await fetchParcels("/case/post1.h5", { group: "A", scalar: "TEMP" });
    expect(parcelsMock).toHaveBeenCalledTimes(3);
  });

  it("does not keep a failed fetch, so a missing group is asked again", async () => {
    parcelsMock.mockRejectedValueOnce(
      Object.assign(new Error("not found"), { status: 404 })
    );
    await expect(
      fetchParcels("/case/post0.h5", { group: "A" })
    ).rejects.toThrow("not found");

    await fetchParcels("/case/post0.h5", { group: "A" });
    expect(parcelsMock).toHaveBeenCalledTimes(2);
  });
});
