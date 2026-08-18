import type { ConvergeKind } from "@/api/files";

/*
 * The CONVERGE output families, shared by the clean prompt and the disk-usage
 * view so the two never drift into naming the same bytes differently.
 */

// Every family the server can report, so each tally it sends has a slot to
// land in whether or not a given view lists it.
export const allConvergeKinds: ConvergeKind[] = [
  "echo",
  "restart",
  "map",
  "out",
  "post",
  "log",
  "run",
  "nfs",
  "outputs",
];

// The globs are the patterns themselves, so they stay verbatim in every locale
// — only any description beside them gets translated.
//
// "nfs" is missing on purpose: the .nfs* stubs are an NFS implementation
// detail, and naming them raises more questions than it answers. They ride
// along with any sweep and are still counted in totals.
export const convergeKinds: { key: ConvergeKind; glob: string }[] = [
  { key: "echo", glob: "*.echo" },
  { key: "restart", glob: "restart*.rst" },
  { key: "map", glob: "map_*.h5" },
  { key: "out", glob: "*.out" },
  { key: "post", glob: "post*.h5, post*.cgns" },
  { key: "log", glob: "*.log" },
  { key: "run", glob: "horizon.json, hosts" },
  { key: "outputs", glob: "outputs_*/" },
];

const globs = new Map(convergeKinds.map((k) => [k.key as string, k.glob]));

// Falls back to the kind's own name, which covers "other" and anything a newer
// server reports that this build has not heard of yet.
export function convergeKindLabel(kind: string): string {
  return globs.get(kind) ?? kind;
}
