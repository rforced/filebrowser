import * as h5 from "@/api/h5";
import { createFrameCache } from "./frameCache";

// A parcel cloud needs no mesh or connectivity, so the browser cost is just
// the point count. Beyond this the server strides the cloud down rather than
// sending everything.
export const PARCEL_LIMIT = 200000;

export interface ParcelRequest {
  group: string;
  scalar?: string;
}

// Parcel clouds are JSON an order of magnitude lighter than surfaces, so more
// frames fit in less memory.
const MAX_ENTRIES = 12;
const MAX_BYTES = 48 << 20;

const cache = createFrameCache<h5.H5ParcelCloud>(
  MAX_ENTRIES,
  MAX_BYTES,
  // Approximate: the decoded numbers dominate, and values/radii ride along
  // with the coordinates.
  (data) =>
    (data.points.length +
      (data.values?.length ?? 0) +
      (data.radius?.length ?? 0)) *
    8
);

const keyOf = (path: string, req: ParcelRequest) =>
  [path, req.group, req.scalar ?? ""].join(" ");

export function fetchParcels(
  path: string,
  req: ParcelRequest
): Promise<h5.H5ParcelCloud> {
  return cache.fetch(keyOf(path, req), (signal) =>
    h5.parcels(
      path,
      req.group,
      { scalar: req.scalar, limit: PARCEL_LIMIT },
      signal
    )
  );
}

export function prefetchParcels(path: string, req: ParcelRequest): void {
  fetchParcels(path, req).catch(() => {});
}

export function clearParcelCache(): void {
  cache.clear();
}
