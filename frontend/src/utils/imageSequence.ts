export interface SequenceStamp {
  frame?: number;
  time?: number;
}

// Catalyst names in-situ frames like
//
// slice1_MASSFRAC_C12H26_000_000330_+2.13747e-03.png
//
// where the last underscore-separated token is the simulation time and the
// one before it the frame number. parseSequenceStamp pulls both out of any
// name following that shape, so the player can caption frames with solver
// time instead of a file name.
const TIME_TOKEN = /^[+-]?\d+(?:\.\d+)?e[+-]?\d+$/i;
const FRAME_TOKEN = /^\d+$/;

export function parseSequenceStamp(name: string): SequenceStamp {
  const stem = name.replace(/\.[^.]+$/, "");
  const tokens = stem.split("_");
  if (tokens.length < 2) return {};

  const stamp: SequenceStamp = {};
  const last = tokens[tokens.length - 1];
  let frameToken = tokens[tokens.length - 2];

  if (TIME_TOKEN.test(last)) {
    stamp.time = Number(last);
  } else if (FRAME_TOKEN.test(last)) {
    // No time in the name; a trailing counter still marks the frame.
    frameToken = last;
  } else {
    return stamp;
  }

  if (FRAME_TOKEN.test(frameToken)) {
    stamp.frame = Number(frameToken);
  }

  return stamp;
}

export function formatSequenceTime(time: number): string {
  return time.toExponential(5);
}
