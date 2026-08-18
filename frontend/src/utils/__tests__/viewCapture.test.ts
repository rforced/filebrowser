import { describe, expect, it } from "vitest";

import { pngFilename } from "../viewCapture";

describe("pngFilename", () => {
  it("swaps the extension for .png", () => {
    expect(pngFilename("surface.dat")).toBe("surface.png");
    expect(pngFilename("post000046_+1.40218e+02.h5")).toBe(
      "post000046_+1.40218e+02.png"
    );
  });

  it("appends the qualifier before the extension", () => {
    expect(pngFilename("post000046.h5", "TEMPERATURE")).toBe(
      "post000046_TEMPERATURE.png"
    );
  });

  it("handles names without an extension", () => {
    expect(pngFilename("model")).toBe("model.png");
  });
});
