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

// These are per-frame budgets, and a still is not a frame. Defaulting to the
// playback ceiling silently holed surfaces that would have fitted whole: a
// 27.4M-cell case measured 2,640,214 boundary triangles, which against the
// 2M step strides by 2 and throws away half the wall to save ~13MB on a
// payload that is only ~50MB drawn entire. The stride drops whole faces
// without closing the gap, so what it buys is never a coarser surface, only a
// holed one — worth it for a sequence, never for the one frame being looked
// at. Anything past what the server will draw is refused rather than thinned,
// and the step menu is where that trade is opted into deliberately.
export const DEFAULT_SURFACE_RESOLUTION: SurfaceResolution = "ultra";

// A frame on screen for a fraction of a second cannot show detail that costs
// seconds to arrive, so playback trades it away and takes it back on pause.
export const PLAYBACK_SURFACE_RESOLUTION: SurfaceResolution = "low";

// The ceiling playback will honour whatever the user picked. A FileSystem box
// is a handful of cores shared with the solver, and the top step asks it to
// cut millions of triangles per frame for a viewer that shows each one for a
// fraction of a second — so the step exists for a still, not for a sequence.
export const PLAYBACK_MAX_RESOLUTION: SurfaceResolution = "high";

// Past this many faces in the mesh a surface is a still and nothing else.
//
// It counts mesh faces rather than drawn triangles because that is what a
// frame actually costs: the whole face table is scanned to find the boundary
// before a stride is even chosen, so a lower detail step buys a smaller
// payload and not a cheaper frame. A 27.4M-cell case measured 84,125,742
// faces and 1.4 seconds a frame on a workstation — on four threads with the
// solver running it is several times that, and playback loops.
//
// Mirrors h5SoloFaces in http/h5_surface.go, where the server stops running
// two extractions of this size at once. A mesh that has to be cut on its own
// is not one to ask for a hundred and twenty times in two minutes.
export const PLAYBACK_MAX_FACES = 16 * 1024 * 1024;

export interface SurfaceRequest {
  stream: string;
  scalar?: string;
  edges?: boolean;
  resolution?: SurfaceResolution;
}

// The entry cap is what playback wants: frames are cheap at the low step —
// 6.4MB on the largest case measured — so eight of them cost little and cover
// a scrub back and forth.
const MAX_ENTRIES = 8;

// The byte cap is what stills want, and they got larger when they stopped
// being strided: the same case is 45MB drawn whole, or 66MB with edges. At the
// old 128MB that was two entries, so flipping between two fields — or turning
// edges on and back off — refetched every time.
//
// Sized for four of them rather than for many: a still is looked at, compared
// against one or two others, and left. Playback is nowhere near this, and a
// surface large enough to fill the budget alone is kept anyway, since evicting
// what was just asked for would turn a large case into a refetch loop.
const MAX_BYTES = 256 << 20;

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
