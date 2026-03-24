import { describe, expect, it } from "vitest";
import {
  availableEncodings,
  decode,
  isEncodableResponse,
} from "../encodings";

describe("availableEncodings", () => {
  it("contains utf-8", () => {
    expect(availableEncodings).toContain("utf-8");
  });

  it("contains common encodings", () => {
    expect(availableEncodings).toContain("iso-8859-1");
    expect(availableEncodings).toContain("shift_jis");
    expect(availableEncodings).toContain("euc-kr");
    expect(availableEncodings).toContain("gbk");
  });

  it("has no duplicate entries", () => {
    const unique = new Set(availableEncodings);
    expect(unique.size).toBe(availableEncodings.length);
  });
});

describe("decode", () => {
  it("decodes a UTF-8 ArrayBuffer to string", () => {
    const encoder = new TextEncoder();
    const buffer = encoder.encode("hello world").buffer;
    expect(decode(buffer, "utf-8")).toBe("hello world");
  });

  it("handles empty buffer", () => {
    const buffer = new ArrayBuffer(0);
    expect(decode(buffer, "utf-8")).toBe("");
  });

  it("decodes UTF-8 content with special characters", () => {
    const encoder = new TextEncoder();
    const buffer = encoder.encode("héllo wörld").buffer;
    expect(decode(buffer, "utf-8")).toBe("héllo wörld");
  });
});

describe("isEncodableResponse", () => {
  it("returns true for .csv files", () => {
    expect(isEncodableResponse("/path/to/file.csv")).toBe(true);
  });

  it("returns false for non-csv files", () => {
    expect(isEncodableResponse("/path/to/file.txt")).toBe(false);
    expect(isEncodableResponse("/path/to/file.json")).toBe(false);
    expect(isEncodableResponse("/path/to/file.html")).toBe(false);
  });

  it("returns false for empty string", () => {
    expect(isEncodableResponse("")).toBe(false);
  });

  it("is case-sensitive", () => {
    expect(isEncodableResponse("/path/to/file.CSV")).toBe(false);
  });
});
