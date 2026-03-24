import { describe, expect, it } from "vitest";
import { removeLastDir, encodeRFC5987ValueChars, encodePath } from "../url";

describe("removeLastDir", () => {
  it("removes the last directory from a path", () => {
    expect(removeLastDir("/a/b/c")).toBe("/a/b");
  });

  it("handles trailing slash", () => {
    expect(removeLastDir("/a/b/c/")).toBe("/a/b");
  });

  it("returns empty string for root path", () => {
    expect(removeLastDir("/")).toBe("");
  });

  it("handles single segment path", () => {
    expect(removeLastDir("/a")).toBe("");
  });

  it("handles path with two segments", () => {
    expect(removeLastDir("/a/b")).toBe("/a");
  });
});

describe("encodeRFC5987ValueChars", () => {
  it("encodes special characters", () => {
    expect(encodeRFC5987ValueChars("file name.txt")).toBe("file%20name.txt");
  });

  it("encodes parentheses and asterisks", () => {
    const result = encodeRFC5987ValueChars("file(1)*");
    expect(result).toContain("%28");
    expect(result).toContain("%29");
    expect(result).toContain("%2A");
  });

  it("preserves pipe, backtick, and caret characters", () => {
    expect(encodeRFC5987ValueChars("|`^")).toBe("|`^");
  });

  it("returns empty string for empty input", () => {
    expect(encodeRFC5987ValueChars("")).toBe("");
  });
});

describe("encodePath", () => {
  it("encodes each path segment separately", () => {
    expect(encodePath("/my folder/my file.txt")).toBe(
      "/my%20folder/my%20file.txt"
    );
  });

  it("preserves slashes", () => {
    expect(encodePath("/a/b/c")).toBe("/a/b/c");
  });

  it("encodes special characters in segments", () => {
    expect(encodePath("/path/with spaces/file#1.txt")).toBe(
      "/path/with%20spaces/file%231.txt"
    );
  });

  it("handles empty string", () => {
    expect(encodePath("")).toBe("");
  });
});
