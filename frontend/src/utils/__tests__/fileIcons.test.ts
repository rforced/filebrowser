import { describe, expect, it } from "vitest";
import { fileIcon, isMutedFile } from "@/utils/fileIcons";

const iconFor = (name: string, type = "blob") =>
  fileIcon({
    name,
    type,
    extension: name.slice(name.lastIndexOf(".")).toLowerCase(),
  }).icon;

describe("fileIcon", () => {
  it("tells the CONVERGE HDF5 payloads apart", () => {
    expect(iconFor("post000013_+1.20003e-02.h5")).toBe("fa-solid fa-cubes");
    expect(iconFor("map.h5")).toBe("fa-solid fa-right-left");
    expect(iconFor("map_1.200025e-02.h5")).toBe("fa-solid fa-right-left");
    expect(iconFor("sl_table.h5")).toBe("fa-solid fa-table-cells");
    expect(iconFor("restart0007.rst")).toBe("fa-solid fa-rotate-right");
    expect(iconFor("chemistry.h5")).toBe("fa-solid fa-database");
  });

  it("colours mapped state and lookup tables as inputs", () => {
    const green = fileIcon({ name: "inputs.in", extension: ".in" }).color;
    expect(fileIcon({ name: "map.h5", extension: ".h5" }).color).toBe(green);
    expect(fileIcon({ name: "sl_table.h5", extension: ".h5" }).color).toBe(
      green
    );
    expect(fileIcon({ name: "post000001_+0.h5", extension: ".h5" }).color).toBe(
      fileIcon({ name: "x.h5", extension: ".h5" }).color
    );
  });

  it("treats every cgns as field data", () => {
    expect(iconFor("slice1_STREAM_00_000009.cgns")).toBe("fa-solid fa-cubes");
  });

  it("keeps surface decks on the model icon", () => {
    expect(iconFor("surface.dat")).toBe("fa-solid fa-cube");
    expect(iconFor("Surface_1234.5.DAT")).toBe("fa-solid fa-cube");
    expect(iconFor("therm.dat", "text")).toBe("fa-solid fa-file-lines");
  });

  it("falls back to extension, then type, then a plain file", () => {
    expect(iconFor("thermo.out", "text")).toBe("fa-solid fa-file-lines");
    expect(fileIcon({ isDir: true, name: "outputs_original" }).icon).toBe(
      "fa-solid fa-folder"
    );
    expect(fileIcon({ name: "photo", type: "image" }).icon).toBe(
      "fa-solid fa-image"
    );
    expect(fileIcon({ name: "hosts", type: "unknown" }).icon).toBe(
      "fa-solid fa-file"
    );
  });
});

describe("isMutedFile", () => {
  it("dims dotfiles and backups", () => {
    expect(isMutedFile({ name: ".nfs0000abc", extension: "" })).toBe(true);
    expect(isMutedFile({ name: "inputs.in.bak", extension: ".bak" })).toBe(
      true
    );
    expect(isMutedFile({ name: "inputs.in", extension: ".in" })).toBe(false);
  });
});
