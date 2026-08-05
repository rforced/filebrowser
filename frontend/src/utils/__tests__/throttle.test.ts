import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { throttle } from "../throttle";

describe("throttle", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("runs the first call immediately", () => {
    const fn = vi.fn();
    throttle(fn, 100)();

    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("does not fire again when the leading call was the only one", () => {
    const fn = vi.fn();
    throttle(fn, 100)();

    vi.advanceTimersByTime(500);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("coalesces calls within a window into one trailing call", () => {
    const fn = vi.fn();
    const throttled = throttle(fn, 100);

    throttled();
    throttled();
    throttled();
    expect(fn).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(100);
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("passes the most recent arguments to the trailing call", () => {
    const fn = vi.fn();
    const throttled = throttle(fn, 100);

    throttled("first");
    throttled("second");
    throttled("third");
    vi.advanceTimersByTime(100);

    expect(fn).toHaveBeenNthCalledWith(1, "first");
    expect(fn).toHaveBeenNthCalledWith(2, "third");
  });

  it("runs immediately again once the window has elapsed", () => {
    const fn = vi.fn();
    const throttled = throttle(fn, 100);

    throttled();
    vi.advanceTimersByTime(100);
    throttled();

    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("keeps a sustained stream to one call per window", () => {
    const fn = vi.fn();
    const throttled = throttle(fn, 100);

    // 500ms of calls every 10ms: one leading plus one per closed window.
    for (let i = 0; i < 50; i++) {
      throttled();
      vi.advanceTimersByTime(10);
    }

    expect(fn).toHaveBeenCalledTimes(6);
  });
});
