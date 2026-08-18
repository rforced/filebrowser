// The colour ramp shared by the 3D views of a post file — the parcel cloud and
// the boundary surface. Both draw a CFD scalar with no inherent zero (a
// temperature, a heat-transfer coefficient, a droplet radius), so the ramp runs
// blue through red and is ordered by lightness as well as hue, which keeps it
// readable when the legend is small and in greyscale.

export type Rgb = [number, number, number];

export const RAMP: Rgb[] = [
  [0.19, 0.31, 0.75],
  [0.27, 0.65, 0.87],
  [0.55, 0.83, 0.62],
  [0.95, 0.79, 0.32],
  [0.84, 0.28, 0.22],
];

// NO_VALUE is what a sample with no usable reading is drawn as: grey, off the
// ramp entirely, so it cannot be mistaken for a value at either end of it. A
// diverged run is exactly when someone opens these views, and NaN arrives from
// the server as null or NaN rather than being dropped.
export const NO_VALUE: Rgb = [0.55, 0.55, 0.58];

export const rampCss = RAMP.map(
  ([r, g, b]) => `rgb(${r * 255} ${g * 255} ${b * 255})`
).join(", ");

/** rampAt samples the ramp at t in [0,1], returning NO_VALUE for a non-finite t. */
export function rampAt(t: number): Rgb {
  if (!Number.isFinite(t)) return NO_VALUE;
  const clamped = Math.min(1, Math.max(0, t));
  const scaled = clamped * (RAMP.length - 1);
  const i = Math.min(RAMP.length - 2, Math.floor(scaled));
  const f = scaled - i;
  const a = RAMP[i];
  const b = RAMP[i + 1];
  return [
    a[0] + (b[0] - a[0]) * f,
    a[1] + (b[1] - a[1]) * f,
    a[2] + (b[2] - a[2]) * f,
  ];
}

/**
 * normalize maps a value onto [0,1] for the ramp. A field that is constant —
 * BOUND_HTC is all zeros in plenty of real post files — has no span to
 * normalize against, so it is drawn at the middle of the ramp rather than at
 * one end, where it would read as uniformly extreme.
 */
export function normalize(value: number, lo: number, hi: number): number {
  if (!Number.isFinite(value)) return NaN;
  const span = hi - lo;
  return span === 0 ? 0.5 : (value - lo) / span;
}
