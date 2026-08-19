import { describe, expect, it, vi, beforeEach } from "vitest";
import { defineComponent, h, KeepAlive } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { useLayoutStore } from "@/stores/layout";
import type { ConvergeCombinePreview } from "@/api/files";

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" } } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

const mockPreview = vi.fn();

vi.mock("@/api", () => ({
  files: {
    convergeCombinePreview: (...args: any[]) => mockPreview(...args),
  },
}));

const mockSummary = vi.fn();

vi.mock("@/utils/convergeSummaryCache", () => ({
  cachedConvergeSummary: (...args: any[]) => mockSummary(...args),
}));

const mockCombineOutput = vi.fn();

vi.mock("@/composables/useFileActions", () => ({
  useFileActions: () => ({ combineOutput: mockCombineOutput }),
}));

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/files/case/" }),
}));

import ConvergeCombine from "../ConvergeCombine.vue";

const messages = {
  buttons: {
    cancel: "Cancel",
    combineConvergeOutput: "Combine output",
  },
  converge: {
    combineExists: "{name} already exists.",
    combineNeedsRuns: "This case has only one output folder.",
    files: "{count} files",
  },
  prompts: {
    convergeCombine: "Combine output",
    convergeCombineEmpty: "No .out files to combine.",
    convergeCombineMessage: "Joins every .out file into {name}.",
    convergeCombineRunning: "This case appears to be running.",
    convergeCombineScanning: "Looking for output files...",
    convergeCombineTotal:
      "{count} files will be written to {name}, up to {size}.",
  },
};

function makePreview(
  overrides: Partial<ConvergeCombinePreview> = {}
): ConvergeCombinePreview {
  return {
    name: "outputs_combined",
    legs: [
      { name: "outputs_original", files: 3, bytes: 1024 },
      { name: "outputs_restart1", files: 2, bytes: 512 },
    ],
    files: 3,
    bytes: 1536,
    exists: false,
    ...overrides,
  };
}

async function mountPrompt(showError = vi.fn()) {
  const pinia = createPinia();
  setActivePinia(pinia);

  const layoutStore = useLayoutStore();
  layoutStore.showHover("converge-combine");

  const i18n = createI18n({
    legacy: false,
    locale: "en",
    messages: { en: messages },
  });

  // onActivated is what the prompt scans from, and it only fires inside a
  // <keep-alive> — which is where Prompts.vue puts it.
  const host = defineComponent({
    render: () => h(KeepAlive, null, [h(ConvergeCombine)]),
  });

  const wrapper = mount(host, {
    global: {
      plugins: [pinia, i18n],
      provide: { $showError: showError },
    },
  });

  await flushPromises();
  return { wrapper, layoutStore, showError };
}

const confirmButton = (wrapper: any) =>
  wrapper.findAll("button").find((b: any) => b.text() === "Combine output")!;

describe("ConvergeCombine", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPreview.mockResolvedValue(makePreview());
    mockSummary.mockResolvedValue({ status: "done" });
  });

  it("lists the legs in the order the server will join them", async () => {
    const { wrapper } = await mountPrompt();

    const items = wrapper.findAll(".combine-legs li");
    expect(items).toHaveLength(2);
    expect(items[0].text()).toContain("outputs_original");
    expect(items[0].text()).toContain("3 files");
    expect(items[1].text()).toContain("outputs_restart1");

    expect(wrapper.text()).toContain(
      "3 files will be written to outputs_combined"
    );
    expect(confirmButton(wrapper).attributes("disabled")).toBeUndefined();
  });

  it("hands the case path to the preview", async () => {
    await mountPrompt();
    expect(mockPreview).toHaveBeenCalledWith("/files/case/", expect.anything());
  });

  it("blocks the combine when the destination already exists", async () => {
    mockPreview.mockResolvedValue(makePreview({ exists: true }));
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("outputs_combined already exists.");
    expect(confirmButton(wrapper).attributes("disabled")).toBeDefined();
  });

  it("blocks the combine when there is a single leg", async () => {
    mockPreview.mockResolvedValue(
      makePreview({
        legs: [{ name: "outputs_original", files: 3, bytes: 1024 }],
      })
    );
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("This case has only one output folder.");
    expect(confirmButton(wrapper).attributes("disabled")).toBeDefined();
  });

  it("blocks the combine when the legs hold no .out files", async () => {
    mockPreview.mockResolvedValue(
      makePreview({
        legs: [
          { name: "outputs_original", files: 0, bytes: 0 },
          { name: "outputs_restart1", files: 0, bytes: 0 },
        ],
        files: 0,
        bytes: 0,
      })
    );
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("No .out files to combine.");
    expect(confirmButton(wrapper).attributes("disabled")).toBeDefined();
  });

  it("warns about a case that is still running", async () => {
    mockSummary.mockResolvedValue({ status: "running" });
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("This case appears to be running.");
    expect(confirmButton(wrapper).attributes("disabled")).toBeUndefined();
  });

  it("closes before starting the combine, so the work is not modal", async () => {
    const { wrapper, layoutStore } = await mountPrompt();

    await confirmButton(wrapper).trigger("click");

    expect(mockCombineOutput).toHaveBeenCalledOnce();
    expect(layoutStore.currentPromptName).toBeUndefined();
  });

  it("reports a failed preview and closes rather than offering a combine", async () => {
    mockPreview.mockRejectedValue(new Error("nope"));
    const showError = vi.fn();
    const { layoutStore } = await mountPrompt(showError);

    expect(showError).toHaveBeenCalledOnce();
    expect(layoutStore.currentPromptName).toBeUndefined();
    expect(mockCombineOutput).not.toHaveBeenCalled();
  });
});
