export interface AnsiSegment {
  text: string;
  class: string;
}

export interface AnsiState {
  fg: number | null;
  bold: boolean;
  dim: boolean;
  italic: boolean;
  underline: boolean;
  pending: string;
}

const ESC = "\u001b";
const BEL = "\u0007";

// Terminal "white" is the normal foreground, not a colour: CONVERGE paints its
// banner with it and it has to stay readable on a light page.
const FOREGROUND: Record<number, string> = {
  30: "text-gray-900 dark:text-gray-100",
  31: "text-red-700 dark:text-red-400",
  32: "text-green-700 dark:text-green-400",
  33: "text-amber-700 dark:text-amber-400",
  34: "text-blue-700 dark:text-blue-400",
  35: "text-fuchsia-700 dark:text-fuchsia-400",
  36: "text-cyan-700 dark:text-cyan-400",
  37: "",
  90: "text-gray-500 dark:text-gray-400",
  91: "text-red-600 dark:text-red-300",
  92: "text-green-600 dark:text-green-300",
  93: "text-amber-600 dark:text-amber-300",
  94: "text-blue-600 dark:text-blue-300",
  95: "text-fuchsia-600 dark:text-fuchsia-300",
  96: "text-cyan-600 dark:text-cyan-300",
  97: "",
};

export function createAnsiState(): AnsiState {
  return {
    fg: null,
    bold: false,
    dim: false,
    italic: false,
    underline: false,
    pending: "",
  };
}

export function segmentClass(state: AnsiState): string {
  const classes: string[] = [];
  if (state.fg !== null) {
    const fg = FOREGROUND[state.fg];
    if (fg) classes.push(fg);
  }
  if (state.bold) classes.push("font-bold");
  if (state.dim) classes.push("opacity-70");
  if (state.italic) classes.push("italic");
  if (state.underline) classes.push("underline");
  return classes.join(" ");
}

const isParameter = (ch: string) => ch >= "0" && ch <= "?";
const isIntermediate = (ch: string) => ch >= " " && ch <= "/";
const isFinal = (ch: string) => ch >= "@" && ch <= "~";

interface Sequence {
  end: number;
  params: string | null;
}

// Reads one escape sequence starting at `start`, where text[start] is ESC.
// Returns null when the sequence runs past the end of the text — the caller
// holds the fragment back until the rest of it arrives.
function readSequence(text: string, start: number): Sequence | null {
  let i = start + 1;
  if (i >= text.length) return null;

  const kind = text[i];

  if (kind === "[") {
    i++;
    const from = i;
    while (i < text.length && isParameter(text[i])) i++;
    const params = text.slice(from, i);
    while (i < text.length && isIntermediate(text[i])) i++;
    if (i >= text.length) return null;
    const final = text[i];
    if (!isFinal(final)) return { end: i + 1, params: null };
    return { end: i + 1, params: final === "m" ? params : null };
  }

  if (
    kind === "]" ||
    kind === "P" ||
    kind === "X" ||
    kind === "^" ||
    kind === "_"
  ) {
    i++;
    while (i < text.length) {
      if (text[i] === BEL) return { end: i + 1, params: null };
      if (text[i] === ESC && i + 1 < text.length && text[i + 1] === "\\") {
        return { end: i + 2, params: null };
      }
      if (text[i] === ESC && i + 1 >= text.length) return null;
      i++;
    }
    return null;
  }

  while (i < text.length && isIntermediate(text[i])) i++;
  if (i >= text.length) return null;
  return { end: i + 1, params: null };
}

function applySgr(state: AnsiState, params: string): void {
  const codes = params.split(";");

  for (let i = 0; i < codes.length; i++) {
    const code = codes[i] === "" ? 0 : Number(codes[i].split(":")[0]);
    if (Number.isNaN(code)) continue;

    if (code === 38 || code === 48) {
      const mode = Number(codes[i + 1]);
      const extended = mode === 5 ? 2 : mode === 2 ? 4 : 1;
      if (code === 38 && mode === 5) {
        const index = Number(codes[i + 2]);
        state.fg = index < 8 ? 30 + index : index < 16 ? 82 + index : null;
      } else if (code === 38) {
        state.fg = null;
      }
      i += extended;
      continue;
    }

    if (code === 0) {
      state.fg = null;
      state.bold = false;
      state.dim = false;
      state.italic = false;
      state.underline = false;
    } else if (code === 1) state.bold = true;
    else if (code === 2) state.dim = true;
    else if (code === 3) state.italic = true;
    else if (code === 4) state.underline = true;
    else if (code === 22) {
      state.bold = false;
      state.dim = false;
    } else if (code === 23) state.italic = false;
    else if (code === 24) state.underline = false;
    else if (code === 39) state.fg = null;
    else if (code in FOREGROUND) state.fg = code;
  }
}

// Appends `chunk` to `out` as styled runs and returns the text it rendered,
// carrying colour across calls so a log polled in pieces renders the same as
// one read whole.
export function pushAnsiSegments(
  out: AnsiSegment[],
  state: AnsiState,
  chunk: string
): string {
  const text = state.pending + chunk;
  state.pending = "";

  let cursor = 0;
  let plain = "";
  let rendered = "";

  const flush = () => {
    if (plain === "") return;
    const cls = segmentClass(state);
    const last = out[out.length - 1];
    if (last && last.class === cls) {
      last.text += plain;
    } else {
      out.push({ text: plain, class: cls });
    }
    rendered += plain;
    plain = "";
  };

  while (cursor < text.length) {
    const esc = text.indexOf(ESC, cursor);
    if (esc === -1) {
      plain += text.slice(cursor);
      break;
    }

    plain += text.slice(cursor, esc);

    const seq = readSequence(text, esc);
    if (seq === null) {
      state.pending = text.slice(esc);
      break;
    }

    if (seq.params !== null) {
      flush();
      applySgr(state, seq.params);
    }
    cursor = seq.end;
  }

  flush();

  return rendered;
}

// Drops `count` characters off the front of `out`. Each run already carries
// its resolved style, so cutting the head cannot change what is left.
export function trimAnsiSegments(out: AnsiSegment[], count: number): void {
  let remaining = count;
  let drop = 0;

  while (drop < out.length && remaining >= out[drop].text.length) {
    remaining -= out[drop].text.length;
    drop++;
  }

  out.splice(0, drop);
  if (remaining > 0 && out.length > 0) {
    out[0].text = out[0].text.slice(remaining);
  }
}

export function ansiSegments(text: string): AnsiSegment[] {
  const out: AnsiSegment[] = [];
  pushAnsiSegments(out, createAnsiState(), text);
  return out;
}

export function stripAnsi(text: string): string {
  if (!text.includes(ESC)) return text;

  let out = "";
  let cursor = 0;

  while (cursor < text.length) {
    const esc = text.indexOf(ESC, cursor);
    if (esc === -1) {
      out += text.slice(cursor);
      break;
    }
    out += text.slice(cursor, esc);
    const seq = readSequence(text, esc);
    if (seq === null) break;
    cursor = seq.end;
  }

  return out;
}
