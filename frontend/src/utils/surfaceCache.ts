import * as h5 from "@/api/h5";
import { createFrameCache } from "./frameCache";

export type SurfaceResolution = "low" | "medium" | "high" | "ultra";

// The lower steps name a triangle budget and the server thins the surface to
// fit it, reporting the stride so the legend can say the wall is partial. The
// top step names none: it asks for the wetted surface as it is, and the server
// draws it whole or refuses it outright, never halves it quietly. A measured
// 2.2M-cell case runs to 8.5M triangles, well past what the lower steps carry.
// Playback is bound by how fast the response crosses the wire, so the lower
// steps are there to buy frames with detail nobody can see at playback size.
export const SURFACE_TRIANGLE_LIMITS: Record<SurfaceResolution, number> = {
  low: 200_000,
  medium: 500_000,
  high: 2_000_000,
  ultra: Infinity,
};

export const DEFAULT_SURFACE_RESOLUTION: SurfaceResolution = "high";

// A frame on screen for a fraction of a second cannot show detail that costs
// seconds to arrive, so playback trades it away and takes it back on pause.
export const PLAYBACK_SURFACE_RESOLUTION: SurfaceResolution = "low";

// The ceiling playback will honour whatever the user picked. A FileSystem box
// is a handful of cores shared with the solver, and the top step asks it to
// cut millions of triangles per frame for a viewer that shows each one for a
// fraction of a second — so the step exists for a still, not for a sequence.
export const PLAYBACK_MAX_RESOLUTION: SurfaceResolution = "high";

export interface SurfaceRequest {
  stream: string;
  scalar?: string;
  edges?: boolean;
  resolution?: SurfaceResolution;
}

const MAX_ENTRIES = 8;
const MAX_BYTES = 128 << 20;

// Every typed-array view is laid over the one response buffer.
const cache = createFrameCache<h5.H5Surface>(
  MAX_ENTRIES,
  MAX_BYTES,
  (data) => data.positions.buffer.byteLength
);

const resolutionOf = (req: SurfaceRequest) =>
  req.resolution ?? DEFAULT_SURFACE_RESOLUTION;

// The resolution belongs in the key: one frame at two steps is two different
// surfaces, and leaving it out would answer a raised setting from the strided
// copy already cached.
const keyOf = (path: string, req: SurfaceRequest) =>
  [
    path,
    req.stream,
    req.scalar ?? "",
    req.edges ? "1" : "",
    resolutionOf(req),
  ].join(" ");

export function fetchSurface(
  path: string,
  req: SurfaceRequest
): Promise<h5.H5Surface> {
  return cache.fetch(keyOf(path, req), (signal) =>
    h5.surface(
      path,
      {
        stream: req.stream,
        scalar: req.scalar,
        limit: SURFACE_TRIANGLE_LIMITS[resolutionOf(req)],
        edges: req.edges,
      },
      signal
    )
  );
}

export function prefetchSurface(path: string, req: SurfaceRequest): void {
  fetchSurface(path, req).catch(() => {});
}

export function abortPendingSurfaces(): void {
  cache.abortPending();
}

export function clearSurfaceCache(): void {
  cache.clear();
}
