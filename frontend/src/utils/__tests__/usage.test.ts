import { describe, expect, it } from "vitest";
import { compressionRatio, usageFraction } from "../usage";

describe("compressionRatio", () => {
  it("reports the ratio of content to footprint", () => {
    // A directory of *.out files, the shape that compresses hardest.
    expect(
      compressionRatio({ size: 78_139_392, logicalSize: 583_472_021 })
    ).toBe("7.47x");
  });

  /*
   * Already-compressed data occupies slightly more than its length once block
   * overhead is counted — measured at 0.96x over a Catalyst PNG sequence. The
   * ratio must survive that rather than assuming allocated is the smaller
   * number.
   */
  it("handles a footprint larger than the content", () => {
    expect(
      compressionRatio({ size: 43_182_080, logicalSize: 41_548_420 })
    ).toBe("0.96x");
  });

  it("stays quiet when compression is not doing anything", () => {
    expect(compressionRatio({ size: 1_000_000, logicalSize: 1_000_000 })).toBe(
      null
    );
  });

  // Below the floor the "ratio" is block rounding on a couple of small files,
  // which tells the user nothing.
  it("stays quiet on samples too small to mean anything", () => {
    expect(compressionRatio({ size: 4096, logicalSize: 10 })).toBe(null);
  });

  it("stays quiet rather than rendering NaN on absent numbers", () => {
    expect(
      compressionRatio({
        size: 4096,
        logicalSize: undefined as unknown as number,
      })
    ).toBe(null);
    expect(compressionRatio({ size: 0, logicalSize: 0 })).toBe(null);
  });
});

describe("usageFraction", () => {
  it("gives the share of the reference value", () => {
    expect(usageFraction(25, 100)).toBe(0.25);
  });

  it("clamps rather than overflowing its bar", () => {
    expect(usageFraction(150, 100)).toBe(1);
    expect(usageFraction(-5, 100)).toBe(0);
  });

  it("survives an empty directory", () => {
    expect(usageFraction(0, 0)).toBe(0);
  });
});
