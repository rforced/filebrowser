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

  const ruler = comments.findIndex((line) =>
    /^#\s*column(\s+\d+)+\s*$/i.test(line)
  );

  for (const line of comments.slice(ruler + 1)) {
    const tokens = line.replace(/^#/, "").trim().split(/\s+/);
    if (tokens.length === 0 || tokens[0] === "") continue;
    if (tokens.length > width) continue;
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

    const tokens = line.split(/\s+/);
    const row = tokens.map(Number);
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
