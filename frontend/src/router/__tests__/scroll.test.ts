import { describe, expect, it } from "vitest";

import { scrollOnNavigate } from "../scroll";

const at = (path: string, query: Record<string, string> = {}) =>
  ({ path, query }) as any;

const scroll = (to: any, from: any, saved: any = null) =>
  scrollOnNavigate(to, from, saved);

describe("scrollOnNavigate", () => {
  it("starts a new directory at the top", () => {
    expect(scroll(at("/files/cases/b/"), at("/files/cases/"))).toEqual({
      top: 0,
    });
  });

  it("starts a file at the top", () => {
    expect(scroll(at("/files/case/inputs.in"), at("/files/case/"))).toEqual({
      top: 0,
    });
  });

  it("leaves an in-view control alone", () => {
    const path = "/files/case/outputs/engine.out";

    expect(scroll(at(path, { runs: "chain" }), at(path))).toBe(false);
    expect(scroll(at(path, { view: "text" }), at(path))).toBe(false);
    expect(scroll(at(path), at(path, { view: "text" }))).toBe(false);
  });

  it("leaves opening and closing the usage panel alone", () => {
    const dir = "/files/cases/";

    expect(scroll(at(dir, { view: "usage" }), at(dir))).toBe(false);
    expect(scroll(at(dir), at(dir, { view: "usage" }))).toBe(false);
  });

  // Back and forward are the one case where the old offset is the right answer.
  it("restores the offset a history entry was left at", () => {
    const saved = { top: 1840, left: 0 };

    expect(scroll(at("/files/cases/"), at("/files/cases/b/"), saved)).toBe(
      saved
    );
  });

  it("treats a sibling frame swap as a normal navigation", () => {
    expect(
      scroll(at("/files/case/post000002.h5"), at("/files/case/post000001.h5"))
    ).toEqual({ top: 0 });
  });
});
