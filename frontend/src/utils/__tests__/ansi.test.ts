import { describe, expect, it } from "vitest";

import {
  ansiSegments,
  createAnsiState,
  pushAnsiSegments,
  stripAnsi,
  trimAnsiSegments,
  type AnsiSegment,
} from "../ansi";

const ESC = "\u001b";

// A verbatim slice of a CONVERGE horizon.log: white banner, magenta progress,
// yellow warning.
const BANNER = `${ESC}[37mTue Aug 18 18:27:49 2026${ESC}[0m\n`;
const READING = `${ESC}[35mreading data from the file engine.in${ESC}[0m\n`;
const WARNING = `${ESC}[33mWARNING: [4010] Network latency is not computed.${ESC}[0m\n`;

describe("stripAnsi", () => {
  it("returns text without escapes untouched", () => {
    expect(stripAnsi("creating pathname /mnt/fs\n")).toBe(
      "creating pathname /mnt/fs\n"
    );
  });

  it("removes the colour codes CONVERGE writes", () => {
    expect(stripAnsi(BANNER + READING + WARNING)).toBe(
      "Tue Aug 18 18:27:49 2026\n" +
        "reading data from the file engine.in\n" +
        "WARNING: [4010] Network latency is not computed.\n"
    );
  });

  it("removes non-colour CSI sequences", () => {
    expect(stripAnsi(`a${ESC}[2Jb${ESC}[1;5Hc${ESC}[?25ld`)).toBe("abcd");
  });

  it("removes OSC sequences ended by BEL or ST", () => {
    expect(stripAnsi(`${ESC}]0;title\u0007run`)).toBe("run");
    expect(stripAnsi(`${ESC}]8;;http://x${ESC}\\run`)).toBe("run");
  });

  it("removes two-character and charset escapes", () => {
    expect(stripAnsi(`${ESC}(Bplain${ESC}=`)).toBe("plain");
  });

  it("drops a sequence truncated by the end of the text", () => {
    expect(stripAnsi(`done${ESC}[3`)).toBe("done");
  });
});

const flat = (segments: AnsiSegment[]) => segments.map((s) => s.text).join("");

describe("ansiSegments", () => {
  it("splits a line into styled runs and keeps every character", () => {
    const segments = ansiSegments(WARNING);
    expect(flat(segments)).toBe(
      "WARNING: [4010] Network latency is not computed.\n"
    );
    expect(segments[0].class).toBe("text-amber-700 dark:text-amber-400");
  });

  it("renders terminal white as the default foreground", () => {
    const segments = ansiSegments(BANNER);
    expect(segments).toHaveLength(1);
    expect(segments[0].class).toBe("");
  });

  it("merges adjacent runs that share a style", () => {
    expect(ansiSegments(`${ESC}[35mone${ESC}[35mtwo`)).toEqual([
      { text: "onetwo", class: "text-fuchsia-700 dark:text-fuchsia-400" },
    ]);
  });

  it("leaves the newline CONVERGE writes after its reset unstyled", () => {
    expect(ansiSegments(READING + READING)).toEqual([
      {
        text: "reading data from the file engine.in",
        class: "text-fuchsia-700 dark:text-fuchsia-400",
      },
      { text: "\n", class: "" },
      {
        text: "reading data from the file engine.in",
        class: "text-fuchsia-700 dark:text-fuchsia-400",
      },
      { text: "\n", class: "" },
    ]);
  });

  it("carries colour across lines until it is reset", () => {
    const segments = ansiSegments(`${ESC}[31mone\ntwo${ESC}[0mthree`);
    expect(segments).toEqual([
      { text: "one\ntwo", class: "text-red-700 dark:text-red-400" },
      { text: "three", class: "" },
    ]);
  });

  it("combines attributes with a colour", () => {
    const segments = ansiSegments(`${ESC}[1;4;36mloud`);
    expect(segments[0].class).toBe(
      "text-cyan-700 dark:text-cyan-400 font-bold underline"
    );
  });

  it("treats an empty parameter list as a reset", () => {
    const segments = ansiSegments(`${ESC}[31mred${ESC}[mplain`);
    expect(segments[1].class).toBe("");
  });

  it("consumes 256-colour and truecolour codes without leaking parameters", () => {
    expect(ansiSegments(`${ESC}[38;5;3mamber`)[0].class).toBe(
      "text-amber-700 dark:text-amber-400"
    );
    expect(ansiSegments(`${ESC}[38;5;200mexotic`)[0].class).toBe("");
    expect(ansiSegments(`${ESC}[38;2;10;20;30mtrue`)[0].class).toBe("");
    expect(flat(ansiSegments(`${ESC}[38;2;10;20;30mtrue`))).toBe("true");
  });

  it("ignores background colours without dropping their text", () => {
    const segments = ansiSegments(`${ESC}[41mon red`);
    expect(segments).toEqual([{ text: "on red", class: "" }]);
  });

  it("drops non-colour CSI sequences from the output", () => {
    expect(flat(ansiSegments(`a${ESC}[2Kb`))).toBe("ab");
  });
});

describe("pushAnsiSegments", () => {
  const feed = (chunks: string[]) => {
    const out: AnsiSegment[] = [];
    const state = createAnsiState();
    for (const chunk of chunks) pushAnsiSegments(out, state, chunk);
    return out;
  };

  it("matches a whole-text parse when a poll splits mid-line", () => {
    expect(feed([`${ESC}[33mWARN`, "ING\n"])).toEqual(
      ansiSegments(`${ESC}[33mWARNING\n`)
    );
  });

  it("holds back an escape sequence split across two chunks", () => {
    expect(feed([`plain${ESC}[3`, `5mreading`])).toEqual(
      ansiSegments(`plain${ESC}[35mreading`)
    );
  });

  it("holds back a bare ESC at the end of a chunk", () => {
    expect(feed([`plain${ESC}`, `[31mred`])).toEqual(
      ansiSegments(`plain${ESC}[31mred`)
    );
  });

  it("keeps colour open across a chunk boundary", () => {
    expect(feed([`${ESC}[35mfirst\n`, `second${ESC}[0m`])).toEqual(
      ansiSegments(`${ESC}[35mfirst\nsecond${ESC}[0m`)
    );
  });

  it("holds back an OSC sequence split across two chunks", () => {
    expect(feed([`${ESC}]0;ti`, `tle\u0007run`])).toEqual(
      ansiSegments(`${ESC}]0;title\u0007run`)
    );
  });

  it("returns the text it rendered, so a caller can mirror the segments", () => {
    const out: AnsiSegment[] = [];
    const state = createAnsiState();
    let mirror = "";
    for (const chunk of [BANNER, `${ESC}[33mWARN`, `ING\n${ESC}[0m`]) {
      mirror += pushAnsiSegments(out, state, chunk);
    }
    expect(mirror).toBe(flat(out));
    expect(mirror).toBe("Tue Aug 18 18:27:49 2026\nWARNING\n");
  });

  it("does not render a held-back fragment until it resolves", () => {
    const out: AnsiSegment[] = [];
    const state = createAnsiState();
    expect(pushAnsiSegments(out, state, `red${ESC}[3`)).toBe("red");
    expect(pushAnsiSegments(out, state, "1mder")).toBe("der");
  });
});

describe("trimAnsiSegments", () => {
  const capped = (text: string, drop: number) => {
    const out = ansiSegments(text);
    trimAnsiSegments(out, drop);
    return out;
  };

  it("drops whole runs that fall entirely in the trimmed head", () => {
    expect(capped(`${ESC}[31mone${ESC}[32mtwo`, 3)).toEqual([
      { text: "two", class: "text-green-700 dark:text-green-400" },
    ]);
  });

  it("keeps the style of a run it cuts into", () => {
    expect(capped(`${ESC}[31mone${ESC}[32mtwo`, 4)).toEqual([
      { text: "wo", class: "text-green-700 dark:text-green-400" },
    ]);
  });

  it("matches a parse of the text that survives the trim", () => {
    const text = BANNER + READING + WARNING;
    expect(flat(capped(text, stripAnsi(BANNER).length))).toBe(
      stripAnsi(READING + WARNING)
    );
  });

  it("empties the list when everything is trimmed", () => {
    expect(capped(READING, 100)).toEqual([]);
  });
});
