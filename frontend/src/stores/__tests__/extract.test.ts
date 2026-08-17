import { describe, it, expect, vi, beforeEach } from "vitest";
import { flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";

vi.mock("@/i18n", () => ({
  default: { global: { t: (key: string) => key } },
}));

vi.mock("@/api", () => ({
  files: {
    extract: vi.fn(),
  },
}));

import { files as api } from "@/api";
import type { ExtractProgress } from "@/api/files";
import { useExtractStore } from "../extract";
import { useFileStore } from "../file";
import { useToastStore } from "../toast";

type ExtractArgs = [
  string,
  object,
  ((progress: ExtractProgress) => void)?,
  (() => void)?,
  AbortSignal?,
];

const extractMock = vi.mocked(api.extract);

describe("extract store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    extractMock.mockReset();
  });

  it("resolves on acceptance and tracks streamed progress", async () => {
    let finish!: () => void;
    extractMock.mockImplementation((...args: ExtractArgs) => {
      const [, , onProgress, onStart] = args;
      onStart?.();
      onProgress?.({
        total: 0,
        current: 3,
        currentFile: "outputs/run.out",
        done: false,
      });
      return new Promise<void>((resolve) => {
        finish = resolve;
      });
    });

    const store = useExtractStore();
    await store.start("/archive.tar.zst", "archive.tar.zst", {});

    expect(store.jobs).toHaveLength(1);
    expect(store.jobs[0].current).toBe(3);
    expect(store.jobs[0].currentFile).toBe("outputs/run.out");

    finish();
    await flushPromises();

    expect(store.jobs).toHaveLength(0);
    expect(useFileStore().reload).toBe(true);
    expect(useToastStore().items).toEqual([
      expect.objectContaining({
        message: "prompts.extractSuccess",
        severity: "success",
      }),
    ]);
  });

  it("rejects without creating a job when the server refuses", async () => {
    extractMock.mockRejectedValue(new Error("destination already exists"));

    const store = useExtractStore();
    await expect(
      store.start("/archive.zip", "archive.zip", {})
    ).rejects.toThrow("destination already exists");

    expect(store.jobs).toHaveLength(0);
    expect(useFileStore().reload).toBe(false);
    expect(useToastStore().items).toHaveLength(0);
  });

  it("reports a mid-stream failure as a toast, not a rejection", async () => {
    let fail!: (e: Error) => void;
    extractMock.mockImplementation((...args: ExtractArgs) => {
      const [, , , onStart] = args;
      onStart?.();
      return new Promise<void>((_, reject) => {
        fail = reject;
      });
    });

    const store = useExtractStore();
    await store.start("/archive.zip", "archive.zip", {});
    fail(new Error("archive exceeds maximum file count"));
    await flushPromises();

    expect(store.jobs).toHaveLength(0);
    expect(useFileStore().reload).toBe(true);
    expect(useToastStore().items).toEqual([
      expect.objectContaining({
        message: "prompts.extractFailed",
        severity: "error",
      }),
    ]);
  });

  it("cancels quietly through the abort signal", async () => {
    extractMock.mockImplementation((...args: ExtractArgs) => {
      const [, , , onStart, signal] = args;
      onStart?.();
      return new Promise<void>((_, reject) => {
        signal?.addEventListener("abort", () =>
          reject(new DOMException("The operation was aborted.", "AbortError"))
        );
      });
    });

    const store = useExtractStore();
    await store.start("/archive.zip", "archive.zip", {});
    store.cancel(store.jobs[0].id);
    await flushPromises();

    expect(store.jobs).toHaveLength(0);
    expect(useFileStore().reload).toBe(true);
    expect(useToastStore().items).toHaveLength(0);
  });
});
