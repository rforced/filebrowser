import { files as api } from "@/api";
import type { ConvergeSummary } from "@/api/files";

const CACHE_TTL = 30 * 1000;

const cache = new Map<string, { summary: ConvergeSummary; at: number }>();

export async function cachedConvergeSummary(
  path: string,
  signal?: AbortSignal
): Promise<ConvergeSummary> {
  const hit = cache.get(path);
  if (hit && Date.now() - hit.at < CACHE_TTL) {
    return hit.summary;
  }

  const summary = await api.convergeSummary(path, signal);
  cache.set(path, { summary, at: Date.now() });
  return summary;
}

export function invalidateConvergeSummary(path: string) {
  cache.delete(path);
}
