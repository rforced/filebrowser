import { describe, expect, it } from "vitest";
import {
  divergenceOf,
  formatBytes,
  formatSimTime,
  formatValue,
  h5KindOf,
  isH5FileName,
  outputProfile,
  projectTotal,
  sequenceFromName,
  simTimeFromName,
} from "@/utils/convergeH5";

describe("h5KindOf", () => {
  it("tells the four CONVERGE payloads apart", () => {
    expect(h5KindOf("post000014_-3.59945e+02.h5")).toBe("post");
    expect(h5KindOf("restart0074.rst")).toBe("restart");
    expect(h5KindOf("map.h5")).toBe("map");
    expect(h5KindOf("map_parcel_-1.201942e+02.h5")).toBe("map");
    expect(h5KindOf("sl_table.h5")).toBe("table");
    expect(h5KindOf("chemistry.h5")).toBe("dataset");
  });

  it("matches regardless of case", () => {
    expect(h5KindOf("POST000001_+0.h5")).toBe("post");
    expect(h5KindOf("Restart0001.RST")).toBe("restart");
  });
});

describe("isH5FileName", () => {
  it("accepts both extensions the reader handles", () => {
    expect(isH5FileName("post000001_+0.00000e+00.h5")).toBe(true);
    expect(isH5FileName("restart0074.rst")).toBe(true);
    expect(isH5FileName("thermo.out")).toBe(false);
    expect(isH5FileName("slice.cgns")).toBe(false);
  });
});

describe("simTimeFromName", () => {
  it("reads the stamped sim time in both sign conventions", () => {
    // v6 engine cases stamp crank-angle degrees, usually negative before TDC.
    expect(simTimeFromName("post000014_-3.59945e+02.h5")).toBeCloseTo(-359.945);
    // v4 transient cases stamp seconds.
    expect(simTimeFromName("post000013_+1.20003e-02.h5")).toBeCloseTo(
      0.0120003
    );
    expect(simTimeFromName("post000001_+0.00000e+00.h5")).toBe(0);
  });

  it("returns null when there is no stamp", () => {
    expect(simTimeFromName("map.h5")).toBeNull();
    expect(simTimeFromName("restart0074.rst")).toBeNull();
    expect(simTimeFromName("post.h5")).toBeNull();
  });
});

describe("sequenceFromName", () => {
  it("reads the write index", () => {
    expect(sequenceFromName("post000014_-3.59945e+02.h5")).toBe(14);
    expect(sequenceFromName("post000001_+0.h5")).toBe(1);
    expect(sequenceFromName("map_1.2e-02.h5")).toBeNull();
  });
});

describe("outputProfile", () => {
  const listing = [
    { name: "post000002_-4.70998e+02.h5", size: 141932972 },
    { name: "post000001_-4.81000e+02.h5", size: 127135516 },
    { name: "post000014_-3.59945e+02.h5", size: 61748680 },
    { name: "thermo.out", size: 654321 },
    { name: "map_-1.2e+02.h5", size: 372599592 },
  ];

  it("keeps only stamped post files, in write order", () => {
    const points = outputProfile(listing);
    expect(points.map((p) => p.sequence)).toEqual([1, 2, 14]);
    expect(points[0].size).toBe(127135516);
    expect(points[2].time).toBeCloseTo(-359.945);
  });

  it("orders by write index, not sim time", () => {
    // A restart can revisit a crank angle; ordering on time alone would fold
    // the series back on itself.
    const points = outputProfile([
      { name: "post000003_-1.00000e+02.h5", size: 30 },
      { name: "post000002_-2.00000e+02.h5", size: 20 },
      { name: "post000004_-1.50000e+02.h5", size: 40 },
    ]);
    expect(points.map((p) => p.sequence)).toEqual([2, 3, 4]);
  });

  it("drops files with no parseable time rather than plotting them at zero", () => {
    expect(outputProfile([{ name: "post.h5", size: 10 }])).toEqual([]);
  });
});

describe("projectTotal", () => {
  const points = outputProfile([
    { name: "post000001_-4.80000e+02.h5", size: 100 },
    { name: "post000002_-4.40000e+02.h5", size: 100 },
    { name: "post000003_-4.00000e+02.h5", size: 100 },
  ]);

  it("scales what is written by the fraction of the deck covered", () => {
    // start -480, end -400 → the run is complete, so nothing extra is coming.
    const got = projectTotal(points, -480, -400);
    expect(got).not.toBeNull();
    expect(got!.written).toBe(300);
    expect(got!.fraction).toBeCloseTo(1);
    expect(got!.projected).toBeCloseTo(300);
  });

  it("projects up when only part of the span is done", () => {
    const got = projectTotal(points, -480, -80);
    expect(got!.fraction).toBeCloseTo(0.2);
    expect(got!.projected).toBeCloseTo(1500);
  });

  it("refuses to guess from an unusable span", () => {
    expect(projectTotal(points, -480, -480)).toBeNull();
    expect(projectTotal(points, NaN, 0)).toBeNull();
    expect(projectTotal(points.slice(0, 1), -480, -80)).toBeNull();
    // A run past its deck end cannot be projected honestly.
    expect(projectTotal(points, -480, -450)).toBeNull();
  });
});

describe("formatSimTime", () => {
  it("marks degrees and seconds differently", () => {
    expect(formatSimTime(-359.94486, "deg")).toBe("-359.94°");
    expect(formatSimTime(0.0120003, "s")).toContain("s");
    expect(formatSimTime(1e-6, "s")).toContain("e-6");
  });

  it("renders bare when the unit is unknown", () => {
    // Restarts and map files carry a sim time but no CRANK_FLAG. Labelling
    // 159.96 crank degrees as "159.963 s" would be actively wrong, so an
    // unqualified value gets no unit at all.
    expect(formatSimTime(159.96257, undefined)).toBe("159.963");
    expect(formatSimTime(159.96257, undefined)).not.toContain("s");
    expect(formatSimTime(-120.1942)).toBe("-120.194");
  });
});

describe("formatValue", () => {
  it("switches notation to suit CFD magnitudes", () => {
    expect(formatValue(101325)).toBe("101325");
    expect(formatValue(1.5e-12)).toBe("1.500e-12");
    expect(formatValue(0)).toBe("0");
    expect(formatValue(NaN)).toBe("NaN");
    expect(formatValue(314.6621398925781)).toBe("314.662");
  });
});

describe("formatBytes", () => {
  it("scales to a readable unit", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(61748680)).toBe("59 MB");
    expect(formatBytes(1536)).toBe("1.5 KB");
  });
});

describe("divergenceOf", () => {
  it("flags non-finite fields, NaN taking priority", () => {
    expect(divergenceOf({ nan: 0, inf: 0 })).toEqual({
      diverged: false,
      count: 0,
    });
    expect(divergenceOf({ nan: 3, inf: 1 })).toEqual({
      diverged: true,
      reason: "nan",
      count: 3,
    });
    expect(divergenceOf({ nan: 0, inf: 7 })).toEqual({
      diverged: true,
      reason: "inf",
      count: 7,
    });
  });
});
