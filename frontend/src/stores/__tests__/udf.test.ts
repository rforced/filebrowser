import { describe, it, expect, vi, beforeEach } from "vitest";
import { flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";

vi.mock("@/i18n", () => ({
  default: { global: { t: (key: string) => key } },
}));

vi.mock("@/api", () => ({
  files: {
    udfBuild: vi.fn(),
  },
}));

// The real module reaches window.FileBrowser through @/utils/constants, which
// no store under test needs. Mocking it keeps the store and the test agreeing
// on one StatusError, which is what the instanceof check turns on.
vi.mock("@/api/utils", () => {
  class StatusError extends Error {
    code?: string;
    constructor(
      message: string,
      public status?: number
    ) {
      super(message);
      this.name = "StatusError";
    }
  }
  return { StatusError };
});

import { files as api } from "@/api";
import type { UdfProgress } from "@/api/files";
import { StatusError } from "@/api/utils";
import { buttonState } from "@/utils/buttons";
import { useUdfStore, udfStartFailure } from "../udf";
import { useFileStore } from "../file";
import { useToastStore } from "../toast";

type BuildArgs = [
  string,
  string,
  ((progress: UdfProgress) => void)?,
  (() => void)?,
  AbortSignal?,
];

const buildMock = vi.mocked(api.udfBuild);

describe("udf store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    buildMock.mockReset();
  });

  it("tracks streamed progress and reports the artifact", async () => {
    let finish!: (final: UdfProgress) => void;
    buildMock.mockImplementation((...args: BuildArgs) => {
      const [, , onProgress, onStart] = args;
      onStart?.();
      onProgress?.({ phase: "configure", percent: 0, line: "-- Configuring" });
      onProgress?.({
        phase: "build",
        percent: 42,
        line: "[ 42%] Building C object",
      });
      return new Promise<UdfProgress | null>((resolve) => {
        finish = resolve;
      });
    });

    const store = useUdfStore();
    await store.start("/pkg", "pkg", "6.0.1");

    expect(store.jobs).toHaveLength(1);
    expect(store.jobs[0].phase).toBe("build");
    expect(store.jobs[0].percent).toBe(42);
    expect(store.jobs[0].version).toBe("6.0.1");
    expect(buttonState("converge-udf")).toBe("loading");

    finish({
      phase: "done",
      percent: 100,
      artifact: "/pkg/libconverge_udf.so",
    });
    await flushPromises();

    expect(store.jobs).toHaveLength(0);
    expect(useFileStore().reload).toBe(true);
    expect(useToastStore().items).toEqual([
      expect.objectContaining({
        message: "prompts.udfSuccess",
        severity: "success",
      }),
    ]);
  });

  // The compile failing is a completed request: the SSE headers went out before
  // the first line of compiler output did, so the outcome rides the last event.
  it("surfaces a compile failure from the terminal event as a sticky toast", async () => {
    buildMock.mockImplementation((...args: BuildArgs) => {
      const [, , , onStart] = args;
      onStart?.();
      return Promise.resolve({
        phase: "done",
        percent: 0,
        error: "src/configure.c:4:1: error: expected ';'",
        logPath: "/pkg/build/compile.log",
      } as UdfProgress);
    });

    const store = useUdfStore();
    await store.start("/pkg", "pkg", "6.0.1");
    await flushPromises();

    expect(store.jobs).toHaveLength(0);
    expect(useToastStore().items).toEqual([
      expect.objectContaining({
        message: "prompts.udfFailed",
        severity: "error",
      }),
    ]);
    expect(buttonState("converge-udf")).toBe("idle");
  });

  // A refused build never becomes a card; the prompt is still open and reports
  // it, so start() rejects rather than toasting.
  it("rejects without a card when the server refuses the build", async () => {
    const refusal = new StatusError("already building", 409);
    refusal.code = "udfBuilding";
    buildMock.mockRejectedValue(refusal);

    const store = useUdfStore();
    await expect(store.start("/pkg", "pkg", "6.0.1")).rejects.toBe(refusal);

    expect(store.jobs).toHaveLength(0);
    expect(useToastStore().items).toHaveLength(0);
    expect(udfStartFailure(refusal)).toBe("prompts.udfAlreadyBuilding");
  });

  it("keeps quiet when the user cancels an accepted build", async () => {
    let signal!: AbortSignal;
    buildMock.mockImplementation((...args: BuildArgs) => {
      const [, , , onStart, abortSignal] = args;
      signal = abortSignal!;
      onStart?.();
      return new Promise<UdfProgress | null>((_, reject) => {
        signal.addEventListener("abort", () => reject(new Error("aborted")));
      });
    });

    const store = useUdfStore();
    await store.start("/pkg", "pkg", "6.0.1");
    expect(store.jobs).toHaveLength(1);

    store.cancel(store.jobs[0].id);
    await flushPromises();

    expect(store.jobs).toHaveLength(0);
    expect(useToastStore().items).toHaveLength(0);
    // The build left objects behind in build/, so the listing is stale.
    expect(useFileStore().reload).toBe(true);
  });
});
