import { describe, expect, it } from "vitest";

import { catchAllRedirect } from "../catchAll";

describe("catchAllRedirect(param)", () => {
  it("regression (vue-router 5.0.7): does NOT throw when the param is undefined", () => {
    // Visiting the root URL "/" under vue-router 5.0.7 makes the catch-all's
    // `to.params.catchAll` come back as `undefined` instead of an empty array.
    // The previous implementation did `[...to.params.catchAll].join("/")`,
    // which threw `TypeError: can't access property Symbol.iterator,
    // e.params.catchAll is undefined` during the initial navigation and took
    // the entire SPA (including the login page) down. This test guards that.
    expect(() => catchAllRedirect(undefined)).not.toThrow();
    expect(catchAllRedirect(undefined)).toBe("/files/");
  });

  it("returns /files/ when the param is null", () => {
    expect(catchAllRedirect(null)).toBe("/files/");
  });

  it("returns /files/ when the param is an empty array", () => {
    expect(catchAllRedirect([])).toBe("/files/");
  });

  it("returns /files/ when the param is an empty string", () => {
    expect(catchAllRedirect("")).toBe("/files/");
  });

  it("wraps a single-string param into the path (non-repeating capture shape)", () => {
    // vue-router 5 with `(.*)` (no trailing `*`) returns a single string that
    // may itself contain slashes — make sure we don't double-encode it.
    expect(catchAllRedirect("foo/bar")).toBe("/files/foo/bar");
  });

  it("joins an array param with `/` (legacy repeating capture shape)", () => {
    // Older router versions / a `(.*)*` pattern hand back an array of segments.
    expect(catchAllRedirect(["a", "b", "c"])).toBe("/files/a/b/c");
  });

  it("preserves a single segment", () => {
    expect(catchAllRedirect("foo")).toBe("/files/foo");
    expect(catchAllRedirect(["foo"])).toBe("/files/foo");
  });
});
