// Helpers for the HDF5 files CONVERGE writes. The container says nothing about
// the payload — post snapshots, restarts, mapped initial conditions and lookup
// tables all share it — so kind is decided by name, matching the icon rules in
// fileIcons.ts and the server's convergeOutputKind.

export type H5Kind = "post" | "restart" | "map" | "table" | "dataset";

// .cgns is HDF5 too: it is what a v6 case writes instead of post*.h5 when
// write_cgns_flag is set, and the endpoint maps it onto the same summary.
export function isH5FileName(name: string): boolean {
  const lower = name.toLowerCase();
  return (
    lower.endsWith(".h5") || lower.endsWith(".rst") || lower.endsWith(".cgns")
  );
}

export function h5KindOf(name: string): H5Kind {
  const lower = name.toLowerCase();
  if (lower.endsWith(".rst")) return "restart";
  if (lower.startsWith("post")) return "post";
  if (lower.startsWith("map")) return "map";
  if (lower.includes("table")) return "table";
  return "dataset";
}

// SIM_TIME_RE pulls the sim time CONVERGE stamps into output filenames, e.g.
// post000014_-3.59945e+02.h5. It is crank-angle degrees or seconds depending on
// the deck, which the filename does not record — only the file's CRANK_FLAG
// attribute does — so callers that have the unit should prefer it.
const SIM_TIME_RE = /_([+-]?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\.(?:h5|cgns)$/;

export function simTimeFromName(name: string): number | null {
  const match = SIM_TIME_RE.exec(name);
  if (match === null) return null;
  const value = Number(match[1]);
  return Number.isFinite(value) ? value : null;
}

// SEQUENCE_RE pulls the write index, which orders files whose sim times repeat
// (a restart can re-emit the same crank angle).
const SEQUENCE_RE = /^post0*(\d+)_/i;

export function sequenceFromName(name: string): number | null {
  const match = SEQUENCE_RE.exec(name);
  if (match === null) return null;
  const value = Number(match[1]);
  return Number.isFinite(value) ? value : null;
}

/**
 * formatSimTime renders a sim time. The unit is optional because only post
 * files record it: restarts and map files carry the number with no CRANK_FLAG,
 * so an unqualified value is rendered bare rather than being labelled with a
 * guess — mislabelling crank degrees as seconds is a real error, not a
 * cosmetic one.
 */
export function formatSimTime(value: number, unit?: "s" | "deg"): string {
  if (unit === "deg") {
    return `${value.toFixed(2)}°`;
  }
  const magnitude =
    value !== 0 && Math.abs(value) < 1e-3
      ? value.toExponential(4)
      : value.toPrecision(6);
  return unit === "s" ? `${magnitude} s` : magnitude;
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[i]}`;
}

export function formatCount(n: number): string {
  return n.toLocaleString();
}

// formatValue renders a field statistic. CFD quantities span pressures in the
// 1e5 range and mass fractions at 1e-12, so neither fixed nor exponential
// notation alone reads well.
export function formatValue(v: number): string {
  if (!Number.isFinite(v)) return String(v);
  if (v === 0) return "0";
  const magnitude = Math.abs(v);
  if (magnitude >= 1e6 || magnitude < 1e-3) return v.toExponential(3);
  return Number(v.toPrecision(6)).toString();
}

export interface DivergenceVerdict {
  diverged: boolean;
  reason?: "nan" | "inf";
  count: number;
}

/**
 * divergenceOf flags a field that has gone non-finite. A NaN or Inf anywhere in
 * a solution field means the solve has blown up, which is worth surfacing from
 * the file listing rather than discovering in a post-processor later.
 */
export function divergenceOf(stats: {
  nan: number;
  inf: number;
}): DivergenceVerdict {
  if (stats.nan > 0) {
    return { diverged: true, reason: "nan", count: stats.nan };
  }
  if (stats.inf > 0) {
    return { diverged: true, reason: "inf", count: stats.inf };
  }
  return { diverged: false, count: 0 };
}

export interface OutputPoint {
  name: string;
  size: number;
  time: number | null;
  sequence: number | null;
}

/**
 * outputProfile turns a listing of an `output/` directory into the mesh-cost
 * profile of a run. Post file size tracks cell count closely, so this reads as
 * mesh growth over the solve without opening a single file — which matters
 * because these are 60-220MB each.
 *
 * Files with no parseable sim time are dropped rather than plotted at zero.
 */
export function outputProfile(
  items: { name: string; size: number }[]
): OutputPoint[] {
  const points: OutputPoint[] = [];
  for (const item of items) {
    const lower = item.name.toLowerCase();
    if (!lower.startsWith("post")) continue;
    // A run writes one format or the other, never both, so no case can mix the
    // two into one sequence.
    if (!lower.endsWith(".h5") && !lower.endsWith(".cgns")) continue;
    const time = simTimeFromName(item.name);
    if (time === null) continue;
    points.push({
      name: item.name,
      size: item.size,
      time,
      sequence: sequenceFromName(item.name),
    });
  }

  // Sequence is the true write order; a restart can revisit a crank angle, so
  // sorting on time alone would fold the series back on itself. A name with no
  // index sorts after the indexed ones rather than being compared on time
  // against them: mixing the two rules makes the order depend on which pair
  // the sort happens to hand the comparator.
  points.sort((a, b) => {
    const sa = a.sequence ?? Number.POSITIVE_INFINITY;
    const sb = b.sequence ?? Number.POSITIVE_INFINITY;
    if (sa !== sb) return sa - sb;
    return (a.time ?? 0) - (b.time ?? 0);
  });
  return points;
}

/**
 * projectTotal estimates what a run will write in total, from what it has
 * written so far against the fraction of the deck it has covered. Returns null
 * when the span is unusable, which is common: v6 decks can drive `twrite_post`
 * from a table of crank ranges rather than a fixed interval, so the cadence is
 * not a single number and only the observed average is honest.
 */
export function projectTotal(
  points: OutputPoint[],
  start: number,
  end: number
): { written: number; projected: number; fraction: number } | null {
  if (points.length < 2 || !Number.isFinite(start) || !Number.isFinite(end)) {
    return null;
  }
  const span = end - start;
  if (span <= 0) return null;

  const written = points.reduce((sum, p) => sum + p.size, 0);
  const reached = points[points.length - 1].time;
  if (reached === null) return null;

  const fraction = (reached - start) / span;
  if (fraction <= 0 || fraction > 1) return null;

  return { written, projected: written / fraction, fraction };
}
