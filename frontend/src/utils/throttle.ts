/**
 * Rate-limits `fn` to at most one call per `wait` ms.
 *
 * Matches lodash's default throttle semantics: the first call in a window runs
 * immediately (leading edge), and if further calls arrive during that window
 * the last one runs when it closes (trailing edge). A window with only the
 * leading call does not fire again on close.
 */
export function throttle<A extends unknown[]>(
  fn: (...args: A) => void,
  wait: number
): (...args: A) => void {
  let lastRun = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let pendingArgs: A | null = null;

  return (...args: A) => {
    const remaining = wait - (Date.now() - lastRun);
    pendingArgs = args;

    if (remaining <= 0) {
      lastRun = Date.now();
      pendingArgs = null;
      fn(...args);
      return;
    }

    if (timer === null) {
      timer = setTimeout(() => {
        timer = null;
        lastRun = Date.now();
        const queued = pendingArgs;
        pendingArgs = null;
        if (queued) fn(...queued);
      }, remaining);
    }
  };
}
