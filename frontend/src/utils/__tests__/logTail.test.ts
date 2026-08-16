import { describe, expect, it } from "vitest";
import {
  capBuffer,
  isLogFileName,
  parseContentRange,
  parseUnsatisfiedRange,
  trimToLine,
} from "@/utils/logTail";

describe("parseContentRange", () => {
  it("reads a 206 range", () => {
    expect(parseContentRange("bytes 100-199/1234")).toEqual({
      start: 100,
      end: 199,
      total: 1234,
    });
  });

  it("rejects everything else", () => {
    expect(parseContentRange("bytes */1234")).toBeNull();
    expect(parseContentRange(null)).toBeNull();
    expect(parseContentRange("garbage")).toBeNull();
  });
});

describe("parseUnsatisfiedRange", () => {
  it("reads the total from a 416", () => {
    expect(parseUnsatisfiedRange("bytes */1234")).toBe(1234);
    expect(parseUnsatisfiedRange("bytes 0-1/2")).toBeNull();
    expect(parseUnsatisfiedRange(null)).toBeNull();
  });
});

describe("trimToLine", () => {
  it("drops the partial first line", () => {
    expect(trimToLine("tail of a line\nfull line\n")).toBe("full line\n");
    expect(trimToLine("no newline at all")).toBe("no newline at all");
    expect(trimToLine("\nstarts clean")).toBe("starts clean");
  });
});

describe("capBuffer", () => {
  it("keeps short buffers and caps long ones at a line boundary", () => {
    expect(capBuffer("short", 100)).toBe("short");
    const text = "aaaa\nbbbb\ncccc\n";
    expect(capBuffer(text, 11)).toBe("bbbb\ncccc\n");
  });
});

describe("isLogFileName", () => {
  it("matches .log case-insensitively", () => {
    expect(isLogFileName("converge.log")).toBe(true);
    expect(isLogFileName("HORIZON.LOG")).toBe(true);
    expect(isLogFileName("catalog")).toBe(false);
    expect(isLogFileName("log.txt")).toBe(false);
  });
});
