import { describe, expect, it, vi, beforeEach } from "vitest";
import { defineComponent, h, KeepAlive } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { useLayoutStore } from "@/stores/layout";
import type { UdfInfo } from "@/api/files";

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" }, t: (key: string) => key } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

const mockInfo = vi.fn();

vi.mock("@/api", () => ({
  files: {
    udfInfo: (...args: any[]) => mockInfo(...args),
  },
}));

const mockStart = vi.fn();

vi.mock("@/stores/udf", () => ({
  useUdfStore: () => ({ start: mockStart }),
  udfStartFailure: (error: unknown) =>
    error instanceof Error ? error.message : String(error),
}));

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/files/v6_UDF_Example/" }),
}));

import UdfCompile from "../UdfCompile.vue";

const messages = {
  buttons: {
    cancel: "Cancel",
    compileUdf: "Compile UDF",
  },
  prompts: {
    udfCompile: "Compile UDF",
    udfCompileMessage: "Compiles the UDF sources in {name}.",
    udfCompileTotal: "Builds against CONVERGE {version} and writes {name}.",
    udfVersion: "CONVERGE version",
    udfScanning: "Looking for installed CONVERGE versions...",
    udfNotPackage: "This folder has no CONVERGE UDF CMakeLists.txt.",
    udfNoVersions: "No installed CONVERGE version can compile a UDF.",
    udfNoSource: "There are no sources in src/.",
  },
};

function makeInfo(overrides: Partial<UdfInfo> = {}): UdfInfo {
  return {
    package: true,
    hasSource: true,
    versions: [
      { version: "6.0.1" },
      { version: "5.1.1" },
      { version: "4.1.2" },
    ],
    ...overrides,
  };
}

async function mountPrompt(showError = vi.fn()) {
  const pinia = createPinia();
  setActivePinia(pinia);

  const layoutStore = useLayoutStore();
  layoutStore.showHover("converge-udf");

  const i18n = createI18n({
    legacy: false,
    locale: "en",
    messages: { en: messages },
  });

  // onActivated is what the prompt loads from, and it only fires inside a
  // <keep-alive> — which is where Prompts.vue puts it.
  const host = defineComponent({
    render: () => h(KeepAlive, null, [h(UdfCompile)]),
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

const compileButton = (wrapper: any) =>
  wrapper.findAll("button").find((b: any) => b.text() === "Compile UDF")!;

describe("UdfCompile", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInfo.mockResolvedValue(makeInfo());
    mockStart.mockResolvedValue(undefined);
  });

  it("offers every installed version, newest first", async () => {
    const { wrapper } = await mountPrompt();

    const options = wrapper.findAll("option");
    expect(options.map((o: any) => o.text())).toEqual([
      "6.0.1",
      "5.1.1",
      "4.1.2",
    ]);
    expect((wrapper.find("select").element as HTMLSelectElement).value).toBe(
      "6.0.1"
    );
    expect(compileButton(wrapper).attributes("disabled")).toBeUndefined();
  });

  // Rebuilding against what the package was last built against is the common
  // case, so it wins over the newest install.
  it("defaults to the version the package was last built against", async () => {
    mockInfo.mockResolvedValue(makeInfo({ lastVersion: "5.1.1" }));
    const { wrapper } = await mountPrompt();

    expect((wrapper.find("select").element as HTMLSelectElement).value).toBe(
      "5.1.1"
    );
  });

  // A version recorded by a since-removed install must not be preselected, or
  // the build would be refused for naming something that is not there.
  it("falls back to the newest when the last version is gone", async () => {
    mockInfo.mockResolvedValue(makeInfo({ lastVersion: "3.0.0" }));
    const { wrapper } = await mountPrompt();

    expect((wrapper.find("select").element as HTMLSelectElement).value).toBe(
      "6.0.1"
    );
  });

  it("blocks the compile when the folder is not a UDF package", async () => {
    mockInfo.mockResolvedValue({
      package: false,
      hasSource: false,
      versions: [],
    });
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("no CONVERGE UDF CMakeLists.txt");
    expect(compileButton(wrapper).attributes("disabled")).toBeDefined();
  });

  it("blocks the compile when no install can build it", async () => {
    mockInfo.mockResolvedValue(makeInfo({ versions: [] }));
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("No installed CONVERGE version");
    expect(compileButton(wrapper).attributes("disabled")).toBeDefined();
  });

  // CONVERGE_BUILD silently falls back to its own samples when src/ is empty,
  // so the build would succeed and produce something nobody asked for.
  it("warns when there are no sources to compile", async () => {
    mockInfo.mockResolvedValue(makeInfo({ hasSource: false }));
    const { wrapper } = await mountPrompt();

    expect(wrapper.text()).toContain("no sources in src/");
    expect(compileButton(wrapper).attributes("disabled")).toBeUndefined();
  });

  it("starts the build with the chosen version and closes", async () => {
    const { wrapper, layoutStore } = await mountPrompt();

    await wrapper.find("select").setValue("5.1.1");
    await compileButton(wrapper).trigger("click");
    await flushPromises();

    expect(mockStart).toHaveBeenCalledWith(
      "/files/v6_UDF_Example/",
      "v6_UDF_Example",
      "5.1.1"
    );
    expect(layoutStore.currentPromptName).toBeUndefined();
  });

  // A refused build leaves the prompt open so the user can pick another
  // version rather than losing what they chose.
  it("stays open and reports when the server refuses the build", async () => {
    mockStart.mockRejectedValue(new Error("already building"));
    const { wrapper, layoutStore, showError } = await mountPrompt();

    await compileButton(wrapper).trigger("click");
    await flushPromises();

    expect(showError).toHaveBeenCalledWith("already building");
    expect(layoutStore.currentPromptName).toBe("converge-udf");
  });
});
