// CONVERGE surface geometry (surface.dat): ASCII, one header line
// `<nverts> <nverts> <ntris>`, then nverts lines `<id> <x> <y> <z>` (1-based,
// sequential), then ntris lines `<v1> <v2> <v3> <boundaryId>`. The name is
// configurable (inputs.in surface_filename) and twrite_surface emits
// time-stamped copies, so files are recognized by content, not name.

export interface SurfaceBoundary {
  id: number;
  triangleCount: number;
  // Non-indexed triangle soup: 9 floats per triangle.
  positions: Float32Array;
}

export interface SurfaceMesh {
  vertexCount: number;
  triangleCount: number;
  boundaries: SurfaceBoundary[];
}

// SurfaceBoundaryInfo is what the model viewer reports upward for the legend.
export interface SurfaceBoundaryInfo {
  id: number;
  triangleCount: number;
  color: string;
}

const HEADER_RE = /^(\d+)\s+(\d+)\s+(\d+)$/;

// firstLine returns the first non-blank line, or null.
const firstLine = (text: string): string | null => {
  let start = 0;
  while (start < text.length) {
    let end = text.indexOf("\n", start);
    if (end === -1) end = text.length;
    const line = text.slice(start, end).trim();
    if (line !== "") return line;
    start = end + 1;
  }
  return null;
};

// sniffSurfaceDat decides whether text looks like a surface file from its
// header alone. Property decks that share the extension (therm.dat, mech.dat,
// liquid.dat, gas.dat) open with a comment line, so this cannot false-match
// them; anything that slips through still fails the strict parse.
export function sniffSurfaceDat(text: string): boolean {
  const line = firstLine(text);
  if (line === null) return false;
  const match = HEADER_RE.exec(line);
  if (match === null) return false;
  return Number(match[1]) > 0 && Number(match[3]) > 0;
}

export function isSurfaceDatName(name: string): boolean {
  const lower = name.toLowerCase();
  return lower.startsWith("surface") && lower.endsWith(".dat");
}

export function isSurfaceDatFile(
  name: string,
  type: string,
  content?: string
): boolean {
  const lower = name.toLowerCase();
  if (!lower.endsWith(".dat")) return false;
  // Past the server's 25 MB text limit a .dat arrives as a blob with no content
  // inlined, so the name is all there is to go on until the viewer fetches it.
  if (type === "blob") return isSurfaceDatName(lower);
  if (type !== "text" && type !== "textImmutable") return false;
  return content !== undefined && sniffSurfaceDat(content);
}

export function parseSurfaceDat(text: string): SurfaceMesh {
  const lines = text.split("\n");
  let cursor = 0;

  const nextLine = (): string | null => {
    while (cursor < lines.length) {
      const line = lines[cursor++].trim();
      if (line !== "") return line;
    }
    return null;
  };

  const header = nextLine();
  const headerMatch = header === null ? null : HEADER_RE.exec(header);
  if (headerMatch === null) {
    throw new Error("not a surface file: bad header");
  }
  const vertexCount = Number(headerMatch[1]);
  const triangleCount = Number(headerMatch[3]);
  if (vertexCount === 0 || triangleCount === 0) {
    throw new Error("not a surface file: empty header counts");
  }

  const vertices = new Float32Array(vertexCount * 3);
  for (let i = 0; i < vertexCount; i++) {
    const line = nextLine();
    if (line === null) throw new Error("truncated vertex list");
    const parts = line.split(/\s+/);
    if (parts.length !== 4) throw new Error("malformed vertex line");
    const id = Number(parts[0]);
    if (!Number.isInteger(id) || id < 1 || id > vertexCount) {
      throw new Error("vertex id out of range");
    }
    const base = (id - 1) * 3;
    for (let axis = 0; axis < 3; axis++) {
      const value = Number(parts[axis + 1]);
      if (Number.isNaN(value)) throw new Error("malformed vertex coordinate");
      vertices[base + axis] = value;
    }
  }

  const triangles = new Uint32Array(triangleCount * 4);
  const counts = new Map<number, number>();
  for (let i = 0; i < triangleCount; i++) {
    const line = nextLine();
    if (line === null) throw new Error("truncated triangle list");
    const parts = line.split(/\s+/);
    if (parts.length !== 4) throw new Error("malformed triangle line");
    const base = i * 4;
    for (let corner = 0; corner < 3; corner++) {
      const vertex = Number(parts[corner]);
      if (!Number.isInteger(vertex) || vertex < 1 || vertex > vertexCount) {
        throw new Error("triangle vertex out of range");
      }
      triangles[base + corner] = vertex;
    }
    const boundary = Number(parts[3]);
    if (!Number.isInteger(boundary) || boundary < 0) {
      throw new Error("malformed boundary id");
    }
    triangles[base + 3] = boundary;
    counts.set(boundary, (counts.get(boundary) ?? 0) + 1);
  }

  if (nextLine() !== null) {
    throw new Error("trailing content after triangle list");
  }

  const boundaries = [...counts.keys()]
    .sort((a, b) => a - b)
    .map((id) => ({
      id,
      triangleCount: counts.get(id)!,
      positions: new Float32Array(counts.get(id)! * 9),
    }));
  const byId = new Map(boundaries.map((b) => [b.id, b]));
  const offsets = new Map(boundaries.map((b) => [b.id, 0]));

  for (let i = 0; i < triangleCount; i++) {
    const base = i * 4;
    const boundary = byId.get(triangles[base + 3])!;
    let offset = offsets.get(boundary.id)!;
    for (let corner = 0; corner < 3; corner++) {
      const vertexBase = (triangles[base + corner] - 1) * 3;
      boundary.positions[offset++] = vertices[vertexBase];
      boundary.positions[offset++] = vertices[vertexBase + 1];
      boundary.positions[offset++] = vertices[vertexBase + 2];
    }
    offsets.set(boundary.id, offset);
  }

  return { vertexCount, triangleCount, boundaries };
}

// boundaryColor spreads hues by golden angle over the *position* in the sorted
// boundary list, so however many boundaries a surface has, neighbours in the
// legend get well-separated colors.
export function boundaryColor(slot: number): {
  h: number;
  s: number;
  l: number;
} {
  return { h: (slot * 137.508) % 360, s: 0.65, l: 0.52 };
}

export function boundaryColorCss(slot: number): string {
  const { h, s, l } = boundaryColor(slot);
  return `hsl(${h.toFixed(1)}, ${s * 100}%, ${l * 100}%)`;
}

// parseBoundaryNames scans a v6 boundary.in (YAML-shaped) for id/name pairs.
// A name binds to the most recent id line, which matches how the deck nests
// them; anything unparseable just yields fewer names.
export function parseBoundaryNames(text: string): Map<number, string> {
  const names = new Map<number, string>();
  let currentId: number | null = null;

  for (const raw of text.split("\n")) {
    const line = raw.split("#")[0].trim();
    const idMatch = /^id:\s+(\d+)$/.exec(line);
    if (idMatch !== null) {
      currentId = Number(idMatch[1]);
      continue;
    }
    const nameMatch = /^name:\s+(.+)$/.exec(line);
    if (nameMatch !== null && currentId !== null) {
      const name = nameMatch[1].trim().replace(/^["']|["']$/g, "");
      if (name !== "" && !names.has(currentId)) {
        names.set(currentId, name);
      }
      currentId = null;
    }
  }

  return names;
}
