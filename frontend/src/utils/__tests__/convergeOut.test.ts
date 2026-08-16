import { describe, expect, it } from "vitest";
import {
  appendOutRows,
  columnLabel,
  isMonotonic,
  isOutFileName,
  parseOutFile,
} from "@/utils/convergeOut";

const DYNAMIC = `# CONVERGE 6.0.1/  Jul 27 2026       Run Date:Sat Aug 15 17:30:27 2026
# column        1                2                3
#           Crank   Tumble_Ratio_X      Swirl_Ratio
#           (deg)           (none)           (none)
#          (none)           (none)           (none)
   -4.8100000e+02   -9.2604050e-06    0.0000000e+00
   -4.8089867e+02   -1.2015625e-05    2.2674814e-06
`;

const PASSIVE = `# CONVERGE 6.0.1/  Jul 27 2026       Run Date:Sat Aug 15 17:30:27 2026
# column        1                2                3
#           Crank         CYLINDER    CYLINDER_Mean
#           (deg)             (kg)         (kg/m^3)
#
   -4.8100000e+02    2.2040662e-04    2.5650610e-01
`;

const AMR = `# CONVERGE 6.0.1/  Jul 27 2026       Run Date:Sat Aug 15 17:30:27 2026
# column        1                2                3
#           Crank    subgrid_scale    subgrid_scale
#           (deg)         VELOCITY      TEMPERATURE
#          (none)         Region_0         Region_3
   -4.8099100e+02    1.0000000e+00   -1.0000000e+00
`;

const CELL_COUNT = `# CONVERGE 6.0.1/  Jul 27 2026       Run Date:Sat Aug 15 17:30:27 2026
# column        1                2                3
#    Cycle_Number      Total_Cells            Rank0
#          (none)           (none)           (none)
#
                0           630275            18974
                6           675191            22274
`;

describe("parseOutFile", () => {
  it("reads names and units from the header block", () => {
    const table = parseOutFile(DYNAMIC);

    expect(table.columns.map(columnLabel)).toEqual([
      "Crank (deg)",
      "Tumble_Ratio_X",
      "Swirl_Ratio",
    ]);
    expect(table.rowCount).toBe(2);
    expect(table.values[0]).toEqual([-481.0, -480.89867]);
    expect(table.values[2][1]).toBeCloseTo(2.2674814e-6);
  });

  it("keeps real units and drops (none)", () => {
    const table = parseOutFile(PASSIVE);

    expect(table.columns[1]).toMatchObject({ name: "CYLINDER", unit: "kg" });
    expect(table.columns[2]).toMatchObject({
      name: "CYLINDER_Mean",
      unit: "kg/m^3",
    });
    expect(table.columns[0].unit).toBe("deg");
  });

  it("merges stacked header rows into qualified names", () => {
    const table = parseOutFile(AMR);

    expect(table.columns[1].name).toBe("subgrid_scale VELOCITY Region_0");
    expect(table.columns[2].name).toBe("subgrid_scale TEMPERATURE Region_3");
    expect(table.columns[1].unit).toBe("");
  });

  it("handles integer columns and a non-time x axis", () => {
    const table = parseOutFile(CELL_COUNT);

    expect(table.columns[0].name).toBe("Cycle_Number");
    expect(table.values[1]).toEqual([630275, 675191]);
  });

  it("skips re-printed headers and mismatched rows mid-file", () => {
    const restarted =
      DYNAMIC +
      `# CONVERGE 6.0.1/  re-print after restart
# column        1                2                3
#           Crank   Tumble_Ratio_X      Swirl_Ratio
   -4.8080000e+02    1.0000000e+00    2.0000000e+00
   -4.8070000e+02    1.0000000e+00
`;
    const table = parseOutFile(restarted);

    expect(table.rowCount).toBe(3);
    expect(table.skippedRows).toBe(1);
    expect(table.columns).toHaveLength(3);
  });

  it("returns an empty table for an empty file", () => {
    const table = parseOutFile("");

    expect(table.columns).toEqual([]);
    expect(table.rowCount).toBe(0);
  });

  it("names columns generically when there is no header", () => {
    const table = parseOutFile("1 2\n3 4\n");

    expect(table.columns.map((c) => c.name)).toEqual(["Column 1", "Column 2"]);
    expect(table.values[1]).toEqual([2, 4]);
  });
});

describe("appendOutRows", () => {
  it("appends matching rows and skips re-headers and malformed lines", () => {
    const table = parseOutFile(DYNAMIC);
    const added = appendOutRows(table, [
      "   -4.8070000e+02    1.0000000e+00    2.0000000e+00",
      "# CONVERGE re-printed header",
      "",
      "   -4.8060000e+02    3.0000000e+00",
      "   -4.8050000e+02    4.0000000e+00    5.0000000e+00",
    ]);

    expect(added).toBe(2);
    expect(table.rowCount).toBe(4);
    expect(table.skippedRows).toBe(1);
    expect(table.values[0].at(-1)).toBeCloseTo(-480.5);
    expect(table.values[2].at(-1)).toBe(5);
  });

  it("adds nothing to an empty table", () => {
    const table = parseOutFile("");
    expect(appendOutRows(table, ["1 2 3"])).toBe(0);
  });
});

describe("isOutFileName", () => {
  it("matches .out case-insensitively and nothing else", () => {
    expect(isOutFileName("dynamic.out")).toBe(true);
    expect(isOutFileName("DYNAMIC.OUT")).toBe(true);
    expect(isOutFileName("dynamic.out.bak")).toBe(false);
    expect(isOutFileName("layout")).toBe(false);
  });
});

describe("isMonotonic", () => {
  it("detects a rewound time axis", () => {
    expect(isMonotonic([1, 2, 2, 3])).toBe(true);
    expect(isMonotonic([1, 2, 1.5])).toBe(false);
    expect(isMonotonic([])).toBe(true);
  });
});
