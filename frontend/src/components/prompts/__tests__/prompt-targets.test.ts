import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { useFileStore } from "@/stores/file";

import en from "@/i18n/en.json";

vi.mock("@/utils/constants", () => ({
  baseURL: "/test",
  origin: "http://localhost",
  name: "Test",
  staticURL: "/static",
  disableExternal: false,
  disableUsedPercentage: false,
  recaptcha: "",
  recaptchaKey: "",
  version: "0.0.0",
  authMethod: "password",
  logoutPage: "",
  theme: "light",
  enableThumbs: false,
  resizePreview: false,
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
  default: { loading: vi.fn(), success: vi.fn(), done: vi.fn() },
}));

vi.mock("@/utils/upload", () => ({ checkConflict: vi.fn() }));

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/files/cases/" }),
  useRouter: () => ({ push: vi.fn() }),
}));

import CopyPrompt from "../Copy.vue";
import MovePrompt from "../Move.vue";
import DownloadPrompt from "../Download.vue";

const FileListStub = {
  name: "FileListStub",
  template: "<div class='file-list-stub'></div>",
  emits: ["update:selected"],
  methods: { createDir: vi.fn() },
};

const items = [
  { url: "/files/cases/run", name: "run", isDir: true, index: 0 },
  { url: "/files/cases/inputs.in", name: "inputs.in", isDir: false, index: 1 },
  { url: "/files/cases/notes.txt", name: "notes.txt", isDir: false, index: 2 },
];

function mountPrompt(component: any, selected: number[]) {
  const pinia = createPinia();
  setActivePinia(pinia);

  const fileStore = useFileStore();
  fileStore.isFiles = true;
  fileStore.req = { name: "cases", isDir: true, items } as any;
  fileStore.selected = selected;

  return mount(component, {
    global: {
      plugins: [
        pinia,
        createI18n({ legacy: false, locale: "en", messages: { en } }),
      ],
      provide: { $showError: vi.fn() },
      stubs: { FileList: FileListStub },
    },
  });
}

const targetNames = (wrapper: ReturnType<typeof mountPrompt>) =>
  wrapper.findAll("li span").map((span) => span.text());

describe("prompt targets", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("lists what is being copied", () => {
    const wrapper = mountPrompt(CopyPrompt, [1, 2]);

    expect(wrapper.text()).toContain(en.prompts.copying);
    expect(targetNames(wrapper)).toEqual(["inputs.in", "notes.txt"]);
  });

  it("lists what is being moved", () => {
    const wrapper = mountPrompt(MovePrompt, [0]);

    expect(wrapper.text()).toContain(en.prompts.moving);
    expect(targetNames(wrapper)).toEqual(["run"]);
  });

  it("lists the selection being downloaded", () => {
    const wrapper = mountPrompt(DownloadPrompt, [0, 2]);

    expect(wrapper.text()).toContain(en.prompts.downloading);
    expect(targetNames(wrapper)).toEqual(["run", "notes.txt"]);
  });

  it("falls back to the current folder when downloading with no selection", () => {
    const wrapper = mountPrompt(DownloadPrompt, []);

    expect(targetNames(wrapper)).toEqual(["cases"]);
  });

  it("marks directories apart from files", () => {
    const wrapper = mountPrompt(DownloadPrompt, [0, 1]);
    const icons = wrapper.findAll("li i").map((icon) => icon.classes());

    expect(icons[0]).toContain("fa-folder");
    expect(icons[1]).toContain("fa-file");
  });
});
