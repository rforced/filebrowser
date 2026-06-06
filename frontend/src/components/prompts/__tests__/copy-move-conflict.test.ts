import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { useFileStore } from "@/stores/file";
import { useLayoutStore } from "@/stores/layout";
import { files as api } from "@/api";
import { checkConflict } from "@/utils/upload";

// Our prompts are Composition API (`<script setup>`), so unlike upstream's
// Options-API test we drive them through the rendered submit button rather than
// calling a `.methods.copy` function directly. The behavior asserted is the
// same: copy/move must await checkConflict with includeDirectories=true, pass
// each item's isDir, and never hit the API while a conflict is pending.

vi.mock("@/utils/constants", () => ({
  baseURL: "/test",
  origin: "http://localhost",
  name: "Test",
  staticURL: "/static",
  disableExternal: false,
  disableUsedPercentage: false,
  recaptcha: "",
  recaptchaKey: "",
  signup: false,
  version: "0.0.0",
  noAuth: false,
  authMethod: "password",
  logoutPage: "",
  loginPage: true,
  theme: "light",
  enableThumbs: false,
  resizePreview: false,
  enableExec: false,
  tusSettings: { retryCount: 5, chunkSize: 10485760 },
  tusEndpoint: "/api/tus",
  logoURL: "/static/img/logo.svg",
  hideLoginButton: false,
  domain: "",
  teamId: "",
  filesystemId: "",
}));

vi.mock("@/i18n", () => ({
  default: { global: { locale: { value: "en" } } },
  detectLocale: () => "en",
  setLocale: () => {},
}));

vi.mock("@/api", () => ({
  files: {
    copy: vi.fn().mockResolvedValue(undefined),
    move: vi.fn().mockResolvedValue(undefined),
    fetchAll: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("@/api/utils", () => ({
  removePrefix: (value: string) => value.replace(/^\/files/, ""),
}));

vi.mock("@/utils/buttons", () => ({
  default: {
    loading: vi.fn(),
    success: vi.fn(),
    done: vi.fn(),
  },
}));

vi.mock("@/utils/upload", () => ({
  checkConflict: vi.fn(),
}));

const mockRoute = { path: "/files/source/" };
vi.mock("vue-router", () => ({
  useRoute: () => mockRoute,
  useRouter: () => ({ push: vi.fn() }),
}));

import CopyPrompt from "../Copy.vue";
import MovePrompt from "../Move.vue";

const conflict = [
  {
    index: 0,
    name: "/target/file.txt",
    origin: { lastModified: undefined, size: 12 },
    dest: { lastModified: "2026-06-04T00:00:00Z", size: 10 },
    checked: ["origin"] as Array<"origin" | "dest">,
    isSmallerOnServer: true,
  },
];

// A minimal stand-in for FileList that lets the test pick the destination by
// emitting update:selected, without FileList's own onMounted api.fetch.
const FileListStub = {
  name: "FileListStub",
  template: "<div class='file-list-stub'></div>",
  emits: ["update:selected"],
  methods: { createDir: vi.fn() },
};

function createI18nPlugin() {
  return createI18n({
    legacy: false,
    locale: "en",
    missing: () => "",
    messages: { en: {} },
  });
}

function mountPrompt(component: typeof CopyPrompt | typeof MovePrompt) {
  const pinia = createPinia();
  setActivePinia(pinia);

  const fileStore = useFileStore();
  fileStore.req = {
    items: [
      {
        url: "/files/source/file.txt",
        name: "file.txt",
        size: 12,
        isDir: false,
        modified: "2026-06-04T00:00:00Z",
      },
    ],
  } as any;
  fileStore.selected = [0];

  const layoutStore = useLayoutStore();
  const showHover = vi
    .spyOn(layoutStore, "showHover")
    .mockImplementation(() => {});

  const wrapper = mount(component, {
    global: {
      plugins: [pinia, createI18nPlugin()],
      provide: { $showError: vi.fn() },
      stubs: { FileList: FileListStub },
    },
  });

  return { wrapper, showHover };
}

describe("copy and move conflict prompts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(checkConflict).mockResolvedValue(conflict);
  });

  it("waits for copy conflict detection before calling the copy API", async () => {
    const { wrapper, showHover } = mountPrompt(CopyPrompt);

    // Pick the destination, as FileList would.
    wrapper
      .getComponent(FileListStub)
      .vm.$emit("update:selected", "/files/target/");
    await wrapper.get("#focus-prompt").trigger("click");
    await flushPromises();

    expect(checkConflict).toHaveBeenCalledWith(
      [
        expect.objectContaining({
          to: "/files/target/file.txt",
          isDir: false,
        }),
      ],
      "/files/target/",
      true
    );
    expect(showHover).toHaveBeenCalledWith(
      expect.objectContaining({
        prompt: "resolve-conflict",
        props: { conflict },
      })
    );
    expect(api.copy).not.toHaveBeenCalled();
  });

  it("waits for move conflict detection before calling the move API", async () => {
    const { wrapper, showHover } = mountPrompt(MovePrompt);

    wrapper
      .getComponent(FileListStub)
      .vm.$emit("update:selected", "/files/target/");
    await wrapper.get("#focus-prompt").trigger("click");
    await flushPromises();

    expect(checkConflict).toHaveBeenCalledWith(
      [
        expect.objectContaining({
          to: "/files/target/file.txt",
          isDir: false,
        }),
      ],
      "/files/target/",
      true
    );
    expect(showHover).toHaveBeenCalledWith(
      expect.objectContaining({
        prompt: "resolve-conflict",
        props: expect.objectContaining({
          conflict,
          files: expect.any(Array),
        }),
      })
    );
    expect(api.move).not.toHaveBeenCalled();
  });
});
