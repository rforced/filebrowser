/*
 * Helpers for talking about storage on a compressed filesystem.
 *
 * Horizon's filesystems are ZFS with zstd, so "how big is this" has two honest
 * answers that can differ by more than an order of magnitude. Measured on real
 * CONVERGE output: *.out compresses 7.5x, *.log 4.6x, *.cgns 1.5x, while
 * already-compressed Catalyst PNGs land at 0.96x — allocated slightly *above*
 * logical once block overhead is counted. Nothing here may assume that
 * allocated is the smaller number.
 */

export interface UsageSizePair {
  size: number;
  logicalSize: number;
}

// Below this the ratio is noise — block rounding on a handful of small files,
// not compression telling you anything.
const RATIO_FLOOR = 64 * 1024;

/*
 * Formats the compression ratio as it is usually quoted (logical per allocated
 * byte, so "2.7x" means the data is 2.7 times its footprint). Returns null when
 * the sample is too small or too near 1x to be worth the words.
 */
export function compressionRatio(usage: UsageSizePair): string | null {
  if (!Number.isFinite(usage.size) || !Number.isFinite(usage.logicalSize)) {
    return null;
  }
  if (usage.size <= 0 || usage.logicalSize <= 0) return null;
  if (usage.logicalSize < RATIO_FLOOR) return null;

  const ratio = usage.logicalSize / usage.size;
  if (ratio >= 0.98 && ratio <= 1.02) return null;

  return `${ratio.toFixed(2)}x`;
}

/*
 * Share of a total, clamped for display. Bars are drawn against the largest
 * row rather than the total so the smaller rows stay visible.
 */
export function usageFraction(value: number, of: number): number {
  if (of <= 0) return 0;
  return Math.max(0, Math.min(1, value / of));
}
