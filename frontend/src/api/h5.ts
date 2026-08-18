import { createURL, fetchJSON } from "./utils";
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
