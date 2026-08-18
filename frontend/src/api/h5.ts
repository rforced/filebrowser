import { createURL, fetchJSON, fetchURL, StatusError } from "./utils";
import url from "@/utils/url";

export interface H5Time {
  value: number;
  // Absent when the file carries no CRANK_FLAG (restarts, map files).
  unit?: "s" | "deg";
  seconds?: number;
  rpm?: number;
}

export interface H5Variable {
  name: string;
  path: string;
  type: string;
  dims: number[];
  bytes: number;
}

export interface H5ParcelGroup {
  name: string;
  path: string;
  count: number;
  variables: string[];
  hasCoords: boolean;
}

export interface H5Stream {
  name: string;
  cells?: number;
  vertices?: number;
  faces?: number;
  variables: H5Variable[];
  parcels?: H5ParcelGroup[];
}

export interface H5Boundary {
  id: number;
  name: string;
  elements: number;
}

export interface H5Summary {
  name: string;
  size: number;
  kind: "post" | "restart" | "map" | "table" | "dataset";
  solver?: string;
  compileDate?: string;
  time?: H5Time;
  cycle?: number;
  ranks?: number;
  restartNumber?: number;
  streams: H5Stream[];
  boundaries?: H5Boundary[];
  truncated?: boolean;
}

export interface H5Stats {
  path: string;
  name: string;
  type: string;
  error?: string;
  count: number;
  min: number;
  max: number;
  mean: number;
  nan: number;
  inf: number;
  finite: number;
}

export interface H5ParcelCloud {
  path: string;
  count: number;
  sent: number;
  stride: number;
  scalar?: string;
  bounds: number[];
  points: number[];
  radius?: (number | null)[];
  // A parcel that has gone non-finite keeps its place in the cloud but has no
  // value to colour by; JSON cannot carry NaN, so it arrives as null.
  values?: (number | null)[];
  range: number[];
  variables: string[];
}

// Callers pass a resource path — `fileStore.req.path`, or the paths the
// converge summary reports — which is already relative to the user's scope.
// It must not be run through removePrefix, which is for `/files/...` route
// paths and would eat the first directory.
const endpoint = (path: string, query = "") =>
  `/api/h5${url.encodePath(path)}${query}`;

export function summary(path: string, signal?: AbortSignal) {
  return fetchJSON<H5Summary>(endpoint(path), { signal });
}

export async function stats(
  path: string,
  datasets: string[],
  signal?: AbortSignal
) {
  const res = await fetchJSON<{ stats: H5Stats[] }>(
    endpoint(path, `?stats=${encodeURIComponent(datasets.join(","))}`),
    { signal }
  );
  return res.stats;
}

export function parcels(
  path: string,
  group: string,
  opts: { scalar?: string; limit?: number } = {},
  signal?: AbortSignal
) {
  const params = new URLSearchParams({ parcels: group });
  if (opts.scalar) params.set("scalar", opts.scalar);
  if (opts.limit) params.set("limit", String(opts.limit));
  return fetchJSON<H5ParcelCloud>(endpoint(path, `?${params}`), { signal });
}

export interface H5SurfaceBoundary {
  id: number;
  name?: string;
  faces: number;
  triangles: number;
  // Where this boundary's triangles sit in the shared index array.
  indexOffset: number;
  indexCount: number;
}

export interface H5SurfaceHeader {
  stream: string;
  vertices: number;
  triangles: number;
  faces: number;
  facesTotal: number;
  stride: number;
  truncated?: boolean;
  // Faces dropped for carrying a corner that could not be placed in the scene.
  skipped?: number;
  bounds: number[];
  scalar?: string;
  range: number[];
  boundaries: H5SurfaceBoundary[];
}

export interface H5Surface extends H5SurfaceHeader {
  positions: Float32Array;
  indices: Uint32Array;
  // Per-vertex, averaged over the boundary faces meeting there. NaN where no
  // adjacent face had a usable value.
  values?: Float32Array;
}

const SURFACE_MAGIC = "FBSURF01";

/**
 * surface fetches the boundary geometry of one stream. The payload is
 * positions and indices in the layout the GPU wants rather than JSON: the
 * largest measured case is 787k triangles, which as JSON would be ~26MB of
 * text and seconds of parsing for the same 14MB of data.
 *
 * The arrays are views over the response buffer, not copies.
 */
export async function surface(
  path: string,
  opts: {
    stream?: string;
    scalar?: string;
    boundaries?: number[];
    limit?: number;
  } = {},
  signal?: AbortSignal
): Promise<H5Surface> {
  const params = new URLSearchParams({ surface: opts.stream ?? "STREAM_00" });
  if (opts.scalar) params.set("scalar", opts.scalar);
  if (opts.boundaries?.length) {
    params.set("boundaries", opts.boundaries.join(","));
  }
  if (opts.limit) params.set("limit", String(opts.limit));

  const res = await fetchURL(endpoint(path, `?${params}`), { signal });
  const buffer = await res.arrayBuffer();
  return parseSurface(buffer);
}

export function parseSurface(buffer: ArrayBuffer): H5Surface {
  if (buffer.byteLength < 12) {
    throw new StatusError("surface response is truncated", 422);
  }
  const magic = new TextDecoder().decode(new Uint8Array(buffer, 0, 8));
  if (magic !== SURFACE_MAGIC) {
    throw new StatusError("not a surface response", 422);
  }

  const headerLength = new DataView(buffer).getUint32(8, true);
  if (12 + headerLength > buffer.byteLength) {
    throw new StatusError("surface header is truncated", 422);
  }
  const header: H5SurfaceHeader = JSON.parse(
    new TextDecoder().decode(new Uint8Array(buffer, 12, headerLength))
  );

  let offset = 12 + headerLength;
  const positions = new Float32Array(buffer, offset, header.vertices * 3);
  offset += header.vertices * 12;
  const indices = new Uint32Array(buffer, offset, header.triangles * 3);
  offset += header.triangles * 12;

  const values = header.scalar
    ? new Float32Array(buffer, offset, header.vertices)
    : undefined;

  return { ...header, positions, indices, values };
}

/**
 * subsetURL is a plain download link rather than a fetch: the response is a
 * CSV attachment and the browser should stream it straight to disk. The token
 * rides in the query because an anchor cannot carry the auth header.
 */
export function subsetURL(path: string, datasets: string[], token: string) {
  return createURL("api/h5" + path, {
    subset: datasets.join(","),
    auth: token,
  });
}
