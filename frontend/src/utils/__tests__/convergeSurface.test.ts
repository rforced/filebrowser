import { describe, expect, it } from "vitest";
import {
  isSurfaceDatFile,
  parseBoundaryNames,
  parseSurfaceDat,
  sniffSurfaceDat,
} from "@/utils/convergeSurface";

const SURFACE = `4 4 3
1 0 0 0
2 1 0 0
3 0 1 0
4 0 0 1
1 2 3 12
2 3 4 12
1 3 4 8
`;

const THERM = `! Cleaned using CONVERGE v3.0.8 $converge cleantherm
! thermo data has been fixed
THERMO ALL
`;

const LIQUID = `#!CONVERGE_VERSION=6
##!Exported by CONVERGE Studio v6 May 12 2026 03:16:59
`;

const BOUNDARY_IN = `version: 6
##!Exported by CONVERGE Studio v6
---

boundary_conditions:
   -  boundary:
         id:              1                # Boundary ID
         type:            WALL
         name:            Piston
         region:          0
   -  boundary:
         id:              2
         type:            WALL
         name:            "Head"
   -  boundary:
         id:              3
         type:            INFLOW
`;

describe("sniffSurfaceDat", () => {
  it("accepts a surface header", () => {
    expect(sniffSurfaceDat(SURFACE)).toBe(true);
    expect(sniffSurfaceDat("\n\n33099 33099 66182\n")).toBe(true);
  });

  it("rejects property decks and junk", () => {
    expect(sniffSurfaceDat(THERM)).toBe(false);
    expect(sniffSurfaceDat(LIQUID)).toBe(false);
    expect(sniffSurfaceDat("")).toBe(false);
    expect(sniffSurfaceDat("1 2\n")).toBe(false);
    expect(sniffSurfaceDat("0 0 0\n")).toBe(false);
    expect(sniffSurfaceDat("1.5 2 3\n")).toBe(false);
  });
});

describe("isSurfaceDatFile", () => {
  it("sniffs text-typed .dat files", () => {
    expect(isSurfaceDatFile("surface.dat", "text", SURFACE)).toBe(true);
    expect(isSurfaceDatFile("surface_1234.5.dat", "text", SURFACE)).toBe(true);
    expect(isSurfaceDatFile("therm.dat", "text", THERM)).toBe(false);
    expect(isSurfaceDatFile("surface.dat", "text", undefined)).toBe(false);
    expect(isSurfaceDatFile("surface.out", "text", SURFACE)).toBe(false);
  });

  it("only trusts the canonical name for blob-typed files", () => {
    expect(isSurfaceDatFile("surface.dat", "blob")).toBe(true);
    expect(isSurfaceDatFile("Surface.DAT", "blob")).toBe(true);
    expect(isSurfaceDatFile("surface_big.dat", "blob")).toBe(false);
  });
});

describe("parseSurfaceDat", () => {
  it("parses vertices and groups triangles by boundary", () => {
    const mesh = parseSurfaceDat(SURFACE);

    expect(mesh.vertexCount).toBe(4);
    expect(mesh.triangleCount).toBe(3);
    expect(mesh.boundaries.map((b) => b.id)).toEqual([8, 12]);

    const b8 = mesh.boundaries[0];
    expect(b8.triangleCount).toBe(1);
    // Triangle 1 3 4 -> vertices (0,0,0), (0,1,0), (0,0,1).
    expect([...b8.positions]).toEqual([0, 0, 0, 0, 1, 0, 0, 0, 1]);

    const b12 = mesh.boundaries[1];
    expect(b12.triangleCount).toBe(2);
    expect(b12.positions).toHaveLength(18);
    expect([...b12.positions.slice(0, 9)]).toEqual([0, 0, 0, 1, 0, 0, 0, 1, 0]);
  });

  it("handles CRLF and out-of-order vertex ids", () => {
    const shuffled = SURFACE.split("\n")
      .map((l) => l + "\r")
      .join("\n")
      .replace("1 0 0 0\r\n2 1 0 0", "2 1 0 0\r\n1 0 0 0");
    const mesh = parseSurfaceDat(shuffled);
    expect([...mesh.boundaries[0].positions]).toEqual([
      0, 0, 0, 0, 1, 0, 0, 0, 1,
    ]);
  });

  it("rejects malformed input", () => {
    expect(() => parseSurfaceDat(THERM)).toThrow();
    expect(() => parseSurfaceDat("4 4 3\n1 0 0 0\n")).toThrow(/truncated/);
    expect(() => parseSurfaceDat(SURFACE + "\nleftover")).toThrow(/trailing/);
    expect(() =>
      parseSurfaceDat(SURFACE.replace("1 2 3 12", "1 2 9 12"))
    ).toThrow(/out of range/);
    expect(() =>
      parseSurfaceDat(SURFACE.replace("1 0 0 0", "1 0 x 0"))
    ).toThrow(/coordinate/);
  });
});

describe("parseBoundaryNames", () => {
  it("maps ids to names, tolerating quotes and gaps", () => {
    const names = parseBoundaryNames(BOUNDARY_IN);
    expect(names.get(1)).toBe("Piston");
    expect(names.get(2)).toBe("Head");
    expect(names.has(3)).toBe(false);
  });

  it("returns an empty map for non-deck input", () => {
    expect(parseBoundaryNames(THERM).size).toBe(0);
  });
});
