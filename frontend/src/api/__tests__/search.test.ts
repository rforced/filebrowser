import { describe, expect, it, vi, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";

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

vi.mock("@/utils/auth", () => ({
  renew: vi.fn(),
  logout: vi.fn(),
}));

import search from "../search";

// The endpoint streams newline-delimited JSON, so the fallback path (no
// pipeThrough) is the one that exercises URL building without a stream mock.
function mockSearchResponse(items: any[]) {
  return vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    statusText: "OK",
    headers: new Headers(),
    body: {},
    text: () => Promise.resolve(items.map((i) => JSON.stringify(i)).join("\n")),
  });
}

async function collect(base: string, items: any[]) {
  globalThis.fetch = mockSearchResponse(items);
  const got: SearchItem[] = [];
  await search(base, "query", new AbortController().signal, (item) =>
    got.push(item)
  );
  return got;
}

describe("search API", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.restoreAllMocks();
  });

  /*
   * The server reports directories as `dir`, not the `isDir` every other
   * resource response uses. Reading the wrong key left folder results without
   * their trailing slash, and route.path is what FileListing joins names onto
   * when pasting — /files/cases/run + name, with no separator.
   */
  it("gives directory results a trailing slash", async () => {
    const [item] = await collect("/files/cases/", [
      { dir: true, path: "run_01/restarts" },
    ]);

    expect(item.url).toBe("/files/cases/run_01/restarts/");
  });

  it("leaves file results without one", async () => {
    const [item] = await collect("/files/cases/", [
      { dir: false, path: "run_01/converge.log" },
    ]);

    expect(item.url).toBe("/files/cases/run_01/converge.log");
  });

  it("encodes names that are not URL-safe", async () => {
    const [item] = await collect("/files/cases/", [
      { dir: true, path: "A Big Archive" },
    ]);

    expect(item.url).toBe("/files/cases/A%20Big%20Archive/");
  });

  it("searches the scope and its subfolders", async () => {
    await collect("/files/cases", []);

    const callUrl = (globalThis.fetch as any).mock.calls[0][0];
    expect(callUrl).toContain("/api/search/cases/?query=query");
  });
});
