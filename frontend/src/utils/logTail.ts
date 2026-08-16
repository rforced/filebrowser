export interface ContentRange {
  start: number;
  end: number;
  total: number;
}

// "bytes 100-199/1234" — a 206 answer naming what it carries.
const RANGE = /^bytes (\d+)-(\d+)\/(\d+)$/;

// "bytes */1234" — a 416 answer naming the file's current size, which is how
// a poll learns "nothing new" (size == offset) from "truncated" (size < offset).
const UNSATISFIED = /^bytes \*\/(\d+)$/;

export function parseContentRange(header: string | null): ContentRange | null {
  const m = RANGE.exec(header ?? "");
  if (!m) return null;
  return { start: Number(m[1]), end: Number(m[2]), total: Number(m[3]) };
}

export function parseUnsatisfiedRange(header: string | null): number | null {
  const m = UNSATISFIED.exec(header ?? "");
  return m ? Number(m[1]) : null;
}

// A tail fetch usually lands mid-line; the fragment before the first newline
// belongs to a line whose start we do not have.
export function trimToLine(text: string): string {
  const nl = text.indexOf("\n");
  return nl === -1 ? text : text.slice(nl + 1);
}

// capBuffer keeps a following session from growing without bound: over the
// cap, the head is dropped back to a line boundary.
export function capBuffer(text: string, max: number): string {
  if (text.length <= max) return text;
  return trimToLine(text.slice(text.length - max));
}

export function isLogFileName(name: string): boolean {
  return name.toLowerCase().endsWith(".log");
}
