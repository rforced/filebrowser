import { files as api } from "@/api";
import type { ConvergeSummary } from "@/api/files";
import { createURL } from "@/api/utils";
import { useAuthStore } from "@/stores/auth";
import {
  parseOutFile,
  stitchOutTables,
  type OutStitch,
  type OutTable,
} from "@/utils/convergeOut";
import { cachedConvergeSummary } from "@/utils/convergeSummaryCache";
import { encodePath, removeLastDir } from "@/utils/url";

export interface ChainLeg {
  runName: string;
  runPath: string;
  filePath: string;
  size: number;
  current: boolean;
}

export interface ChainContext {
  caseRoot: string;
  remainder: string;
  legs: ChainLeg[];
  newestRunName: string;
  newestRunPath: string;
  newestHasFile: boolean;
}

export interface ChainFetch {
  stitch: OutStitch;
  totalLegs: number;
}

export interface NewestRunFile {
  runName: string;
  runPath: string;
  filePath: string;
}

const PROBE_LIMIT = 24;

// All paths here are resource paths as the server reports them; the listing
// and summary APIs want an encoded /files router URL instead.
const routerUrl = (path: string) => "/files" + encodePath(path);

async function probeRun(
  runPath: string,
  remainderDir: string,
  fileName: string,
  signal?: AbortSignal
): Promise<number | null> {
  try {
    const listing = await api.fetch(routerUrl(runPath + remainderDir), signal);
    const item = (listing.items ?? []).find(
      (entry) => !entry.isDir && entry.name === fileName
    );
    return item ? item.size : null;
  } catch {
    return null;
  }
}

async function caseSummary(
  caseRoot: string,
  signal?: AbortSignal
): Promise<ConvergeSummary | null> {
  try {
    return await cachedConvergeSummary(routerUrl(caseRoot), signal);
  } catch {
    return null;
  }
}

export async function discoverChain(
  filePath: string,
  size: number,
  signal?: AbortSignal
): Promise<ChainContext | null> {
  const runDir = removeLastDir(removeLastDir(filePath));
  if (runDir === "") return null;
  const caseRoot = removeLastDir(runDir);
  const remainder = filePath.slice(runDir.length);
  const remainderDir = removeLastDir(remainder);
  const fileName = remainder.slice(remainder.lastIndexOf("/") + 1);

  const summary = await caseSummary(caseRoot, signal);
  if (!summary) return null;

  const runs = summary.runs ?? [];
  if (!summary.isCase || runs.length < 2) return null;
  if (!runs.some((run) => run.path === runDir)) return null;

  const probed = runs.slice(0, PROBE_LIMIT);
  const sizes = await Promise.all(
    probed.map((run) =>
      run.path === runDir
        ? Promise.resolve(size)
        : probeRun(run.path, remainderDir, fileName, signal)
    )
  );
  if (signal?.aborted) return null;

  const legs: ChainLeg[] = [];
  for (let i = probed.length - 1; i >= 0; i--) {
    const legSize = sizes[i];
    if (legSize === null) continue;
    legs.push({
      runName: probed[i].name,
      runPath: probed[i].path,
      filePath: probed[i].path + remainder,
      size: legSize,
      current: probed[i].path === runDir,
    });
  }
  if (legs.length < 2) return null;

  return {
    caseRoot,
    remainder,
    legs,
    newestRunName: runs[0].name,
    newestRunPath: runs[0].path,
    newestHasFile: sizes[0] !== null,
  };
}

export function selectLegsWithinBudget(
  legs: ChainLeg[],
  maxBytes: number
): ChainLeg[] {
  const fitting = legs.filter((leg) => leg.size <= maxBytes);
  let total = fitting.reduce((sum, leg) => sum + leg.size, 0);
  let start = 0;
  while (total > maxBytes && start < fitting.length - 1) {
    total -= fitting[start].size;
    start++;
  }
  return fitting.slice(start);
}

export async function fetchChain(
  ctx: ChainContext,
  maxBytes: number,
  current: { path: string; table: OutTable } | null,
  signal?: AbortSignal
): Promise<ChainFetch | { error: "mismatch" | "empty" }> {
  const kept = selectLegsWithinBudget(ctx.legs, maxBytes);

  const tables = await Promise.all(
    kept.map(async (leg): Promise<OutTable | null> => {
      if (current && leg.filePath === current.path) return current.table;
      try {
        const res = await fetch(
          createURL("api/raw" + leg.filePath, {
            auth: useAuthStore().token,
            inline: "true",
          }),
          { cache: "no-store", signal }
        );
        if (!res.ok) return null;
        return parseOutFile(await res.text());
      } catch {
        return null;
      }
    })
  );
  if (signal?.aborted) throw new DOMException("Aborted", "AbortError");

  const parts = kept.flatMap((leg, i) => {
    const table = tables[i];
    return table && table.rowCount > 0
      ? [{ name: leg.runName, path: leg.filePath, table }]
      : [];
  });
  if (parts.length === 0) return { error: "empty" };

  const stitch = stitchOutTables(parts);
  if (stitch === null) return { error: "mismatch" };
  return { stitch, totalLegs: ctx.legs.length };
}

export async function checkNewestRun(
  filePath: string,
  signal?: AbortSignal
): Promise<NewestRunFile | null> {
  const runDir = removeLastDir(removeLastDir(filePath));
  if (runDir === "") return null;
  const remainder = filePath.slice(runDir.length);

  const summary = await caseSummary(removeLastDir(runDir), signal);
  const newest = summary?.runs?.[0];
  if (!summary?.isCase || !newest || newest.path === runDir) return null;

  const size = await probeRun(
    newest.path,
    removeLastDir(remainder),
    remainder.slice(remainder.lastIndexOf("/") + 1),
    signal
  );
  if (size === null) return null;

  return {
    runName: newest.name,
    runPath: newest.path,
    filePath: newest.path + remainder,
  };
}
