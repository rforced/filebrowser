export interface OutColumn {
  index: number;
  name: string;
  unit: string;
}

export interface OutTable {
  columns: OutColumn[];
  values: number[][];
  rowCount: number;
  skippedRows: number;
}

const isComment = (line: string) => line.startsWith("#");

const UNIT_TOKEN = /^\((.*)\)$/;

const RULER = /^#\s*column((?:\s+\d+)+)\s*$/i;

// time.out closes each row with a dt_limiter tag written as "#dt_grow" or
// "#dt_piso, max_piso_recover reached" — free text, sometimes several tokens,
// always introduced by a hash. Cutting there leaves the numeric fields; a row
// whose limiter is empty is unaffected.
const dataFields = (line: string): string[] => {
  const hash = line.indexOf("#");
  const data = hash === -1 ? line : line.slice(0, hash).trimEnd();
  return data === "" ? [] : data.split(/\s+/);
};

function mergeHeaderRow(columns: OutColumn[], tokens: string[]) {
  for (let c = 0; c < columns.length && c < tokens.length; c++) {
    const unit = UNIT_TOKEN.exec(tokens[c]);
    if (unit) {
      if (columns[c].unit === "" && unit[1] !== "none" && unit[1] !== "") {
        columns[c].unit = unit[1];
      }
      continue;
    }

    columns[c].name =
      columns[c].name === "" ? tokens[c] : `${columns[c].name} ${tokens[c]}`;
  }
}

function headerColumns(comments: string[], width: number): OutColumn[] {
  const columns: OutColumn[] = Array.from({ length: width }, (_, index) => ({
    index,
    name: "",
    unit: "",
  }));

  const ruler = comments.findIndex((line) => RULER.test(line));

  // The ruler is what the file says it writes, which can exceed the numeric
  // width when the last column is free text. Header rows are allowed up to that
  // count so the names still land on the columns that do carry numbers.
  const declared = Math.max(
    width,
    ruler === -1
      ? 0
      : RULER.exec(comments[ruler])![1].trim().split(/\s+/).length
  );

  for (const line of comments.slice(ruler + 1)) {
    const tokens = line.replace(/^#/, "").trim().split(/\s+/);
    if (tokens.length === 0 || tokens[0] === "") continue;
    if (tokens.length > declared) continue;
    mergeHeaderRow(columns, tokens);
  }

  for (const column of columns) {
    if (column.name === "") {
      column.name = `Column ${column.index + 1}`;
    }
  }

  return columns;
}

export function parseOutFile(text: string): OutTable {
  const lines = text.split("\n");

  let comments: string[] = [];
  let columns: OutColumn[] | null = null;
  let values: number[][] = [];
  let rowCount = 0;
  let skippedRows = 0;

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (line === "") continue;

    if (isComment(line)) {
      if (columns === null) comments.push(line);
      continue;
    }

    const row = dataFields(line).map(Number);
    if (row.some((v) => !Number.isFinite(v))) {
      skippedRows++;
      continue;
    }

    if (columns === null) {
      columns = headerColumns(comments, row.length);
      comments = [];
      values = columns.map(() => []);
    }

    if (row.length !== columns.length) {
      skippedRows++;
      continue;
    }

    for (let c = 0; c < row.length; c++) {
      values[c].push(row[c]);
    }
    rowCount++;
  }

  return {
    columns: columns ?? [],
    values,
    rowCount,
    skippedRows,
  };
}

export function isOutFileName(name: string): boolean {
  return name.toLowerCase().endsWith(".out");
}

// appendOutRows feeds complete lines that arrived after the initial parse
// into an existing table. Comment lines (a restart re-printing its header)
// are skipped and rows that do not match the established width are counted,
// mirroring parseOutFile. Returns how many rows were added.
export function appendOutRows(table: OutTable, lines: string[]): number {
  const width = table.columns.length;
  if (width === 0) return 0;

  let added = 0;
  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (line === "" || line.startsWith("#")) continue;

    const row = dataFields(line).map(Number);
    if (row.length !== width || row.some((v) => !Number.isFinite(v))) {
      table.skippedRows++;
      continue;
    }

    for (let c = 0; c < width; c++) {
      table.values[c].push(row[c]);
    }
    table.rowCount++;
    added++;
  }

  return added;
}

export interface OutSegment {
  name: string;
  path: string;
  startRow: number;
}

export interface OutStitch {
  table: OutTable;
  segments: OutSegment[];
  trimmedRows: number;
  droppedLegs: string[];
}

// stitchOutTables concatenates the same .out file across run directories,
// oldest leg first. Column 0 is the solver's own progression axis: a leg
// restarted from an earlier checkpoint supersedes every accumulated row at or
// past its first value. The row equal to it is the checkpoint row the new leg
// rewrites verbatim and drops silently; strictly greater rows are counted in
// trimmedRows. Legs left without rows land in droppedLegs. Returns null when
// the legs do not agree on column names.
export function stitchOutTables(
  legs: { name: string; path: string; table: OutTable }[]
): OutStitch | null {
  const parts = legs.filter((leg) => leg.table.rowCount > 0);
  if (parts.length === 0) return null;

  const shape = parts[0].table.columns.map((c) => c.name).join("\n");
  for (const part of parts) {
    if (part.table.columns.map((c) => c.name).join("\n") !== shape) {
      return null;
    }
  }

  const columns = parts[0].table.columns.map((c) => ({ ...c }));
  for (const part of parts) {
    for (const column of columns) {
      if (column.unit === "") {
        column.unit = part.table.columns[column.index].unit;
      }
    }
  }

  const values: number[][] = columns.map(() => []);
  const segments: OutSegment[] = [];
  const droppedLegs: string[] = [];
  let trimmedRows = 0;
  let skippedRows = 0;

  for (const part of parts) {
    skippedRows += part.table.skippedRows;

    const start = part.table.values[0][0];
    let cut = values[0].length;
    while (cut > 0 && values[0][cut - 1] >= start) {
      if (values[0][cut - 1] > start) trimmedRows++;
      cut--;
    }
    if (cut < values[0].length) {
      for (const column of values) column.length = cut;
      while (
        segments.length > 0 &&
        segments[segments.length - 1].startRow >= cut
      ) {
        droppedLegs.push(segments.pop()!.name);
      }
    }

    segments.push({
      name: part.name,
      path: part.path,
      startRow: values[0].length,
    });
    for (let c = 0; c < values.length; c++) {
      const source = part.table.values[c];
      for (let i = 0; i < source.length; i++) {
        values[c].push(source[i]);
      }
    }
  }

  return {
    table: { columns, values, rowCount: values[0].length, skippedRows },
    segments,
    trimmedRows,
    droppedLegs,
  };
}

export function columnLabel(column: OutColumn): string {
  return column.unit === "" ? column.name : `${column.name} (${column.unit})`;
}

export function isMonotonic(values: number[]): boolean {
  for (let i = 1; i < values.length; i++) {
    if (values[i] < values[i - 1]) return false;
  }
  return true;
}

export function formatOutValue(value: number): string {
  if (value === 0) return "0";
  if (!Number.isFinite(value)) return String(value);

  const magnitude = Math.abs(value);
  if (magnitude >= 1e5 || magnitude < 1e-3) {
    return value.toExponential(3);
  }
  return String(Number(value.toPrecision(6)));
}
