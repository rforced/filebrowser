import { describe, expect, it } from "vitest";

import { NO_VALUE, RAMP, normalize, rampAt } from "@/utils/colorRamp";

describe("rampAt", () => {
  it("returns the ramp ends at the extremes", () => {
    expect(rampAt(0)).toEqual(RAMP[0]);
    expect(rampAt(1)).toEqual(RAMP[RAMP.length - 1]);
  });

  it("clamps outside [0,1] rather than running off the ramp", () => {
    expect(rampAt(-5)).toEqual(RAMP[0]);
    expect(rampAt(5)).toEqual(RAMP[RAMP.length - 1]);
  });

  // A diverged run is exactly when someone opens these views, so a
  // non-finite sample has to be drawn as "no reading" rather than as a value
  // at one end of the legend.
  it("draws a non-finite sample off the ramp", () => {
    expect(rampAt(NaN)).toEqual(NO_VALUE);
    expect(rampAt(Infinity)).toEqual(NO_VALUE);
    expect(NO_VALUE).not.toEqual(RAMP[0]);
    expect(NO_VALUE).not.toEqual(RAMP[RAMP.length - 1]);
  });

  it("interpolates between stops", () => {
    const mid = rampAt(0.5);
    expect(mid).toEqual(RAMP[2]);

    const quarter = rampAt(0.125);
    for (let i = 0; i < 3; i++) {
      expect(quarter[i]).toBeCloseTo((RAMP[0][i] + RAMP[1][i]) / 2, 6);
    }
  });
});

describe("normalize", () => {
  it("maps a value onto its span", () => {
    expect(normalize(10, 0, 100)).toBeCloseTo(0.1, 6);
    expect(normalize(0, 0, 100)).toBe(0);
    expect(normalize(100, 0, 100)).toBe(1);
  });

  // BOUND_HTC reads all-zero in plenty of real post files. Dividing by a zero
  // span would put every sample at one end of the ramp, so a constant field is
  // drawn at the middle instead of as uniformly extreme.
  it("puts a constant field in the middle of the ramp", () => {
    expect(normalize(0, 0, 0)).toBe(0.5);
    expect(normalize(42, 42, 42)).toBe(0.5);
    expect(rampAt(normalize(0, 0, 0))).toEqual(RAMP[2]);
  });

  it("passes a non-finite value through as NaN", () => {
    expect(Number.isNaN(normalize(NaN, 0, 1))).toBe(true);
    expect(Number.isNaN(normalize(Infinity, 0, 1))).toBe(true);
  });
});
