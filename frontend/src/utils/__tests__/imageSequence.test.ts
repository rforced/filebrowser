import { describe, expect, it } from "vitest";
import { parseSequenceStamp } from "@/utils/imageSequence";

describe("parseSequenceStamp", () => {
  it("reads frame and time from a Catalyst image name", () => {
    const stamp = parseSequenceStamp(
      "slice1_MASSFRAC_C12H26_000_000330_+2.13747e-03.png"
    );
    expect(stamp.frame).toBe(330);
    expect(stamp.time).toBeCloseTo(2.13747e-3);
  });

  it("handles negative times such as crank angles", () => {
    const stamp = parseSequenceStamp("slice2_TEMP_000_000042_-1.43111e+02.png");
    expect(stamp.time).toBeCloseTo(-143.111);
    expect(stamp.frame).toBe(42);
  });

  it("falls back to a trailing frame counter", () => {
    expect(parseSequenceStamp("frame_000123.png")).toEqual({ frame: 123 });
  });

  it("returns nothing for ordinary photos", () => {
    expect(parseSequenceStamp("IMG_20260815_vacation.jpg")).toEqual({});
    expect(parseSequenceStamp("photo.png")).toEqual({});
  });
});
