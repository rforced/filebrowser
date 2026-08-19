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

import {
  dirSize,
  usageBreakdown,
  checksum,
  convergeCombinePreview,
  getCombinedDownloadURL,
} from "../files";

function mockFetchResponse(body: any, status = 200) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: "OK",
    headers: new Headers(),
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  });
}

describe("files API", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.restoreAllMocks();
  });

  describe("dirSize", () => {
    /*
     * dirSize takes a RESOURCE path, the form the server reports in
     * FileInfo.path — not a /files/... router URL. It must not go through
     * removePrefix(), which drops the first two segments and would rewrite
     * /cases/foo to /foo and collapse /cases to the root, taking the query
     * string with the segment it drops.
     */
    it("addresses the resource path directly", async () => {
      globalThis.fetch = mockFetchResponse({});

      await dirSize("/cases/george_v4_catalyst");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/resources/cases/george_v4_catalyst");
      expect(callUrl).toContain("dirsize=true");
    });

    // A single-segment path is the case that hid the bug: it is the only shape
    // removePrefix mangles into something that still resolves.
    it("keeps a top-level directory addressable", async () => {
      globalThis.fetch = mockFetchResponse({});

      await dirSize("/cases");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/resources/cases?dirsize=true");
    });

    it("encodes names that are not URL-safe", async () => {
      globalThis.fetch = mockFetchResponse({});

      await dirSize("/cases/A Big Archive");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/resources/cases/A%20Big%20Archive");
      expect(callUrl).not.toContain("A Big Archive");
    });

    it("returns both the allocated and the logical size", async () => {
      const dirInfo = {
        size: 4096,
        logicalSize: 12000,
        numFiles: 10,
        numDirs: 3,
      };
      globalThis.fetch = mockFetchResponse(dirInfo);

      const result = await dirSize("/my-folder");

      expect(result).toEqual(dirInfo);
      expect(result.size).toBe(4096);
      expect(result.logicalSize).toBe(12000);
    });

    it("handles an empty directory response", async () => {
      const dirInfo = { size: 0, logicalSize: 0, numFiles: 0, numDirs: 0 };
      globalThis.fetch = mockFetchResponse(dirInfo);

      expect(await dirSize("/empty")).toEqual(dirInfo);
    });

    it("sends a GET request", async () => {
      globalThis.fetch = mockFetchResponse({});

      await dirSize("/test");

      const callOpts = (globalThis.fetch as any).mock.calls[0][1];
      expect(callOpts.method ?? "GET").toBe("GET");
    });
  });

  describe("usageBreakdown", () => {
    it("addresses the resource path directly", async () => {
      globalThis.fetch = mockFetchResponse({ children: [] });

      await usageBreakdown("/cases");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/usage/breakdown/cases");
      // The bug this guards: /cases collapsing to the root, so descending into
      // a directory kept showing its parent's children.
      expect(callUrl).not.toMatch(/breakdown\/?$/);
    });

    it("encodes names that are not URL-safe", async () => {
      globalThis.fetch = mockFetchResponse({ children: [] });

      await usageBreakdown("/cases/A Big Archive");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/usage/breakdown/cases/A%20Big%20Archive");
    });

    it("asks for the kind rollup only when wanted", async () => {
      globalThis.fetch = mockFetchResponse({ children: [] });
      await usageBreakdown("/cases");
      expect((globalThis.fetch as any).mock.calls[0][0]).not.toContain("kinds");

      globalThis.fetch = mockFetchResponse({ children: [] });
      await usageBreakdown("/cases", { kinds: true });
      expect((globalThis.fetch as any).mock.calls[0][0]).toContain(
        "kinds=true"
      );
    });
  });

  describe("convergeCombinePreview", () => {
    /*
     * The prompt hands over the route it is open on, a /files/... router URL,
     * so this one does go through removePrefix — unlike the download below,
     * which is addressed by resource path.
     */
    it("strips the router prefix off the case it previews", async () => {
      globalThis.fetch = mockFetchResponse({ legs: [] });

      await convergeCombinePreview("/files/cases/engine/");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/combine/cases/engine/");
    });
  });

  describe("getCombinedDownloadURL", () => {
    /*
     * Addressed by RESOURCE path, the form FileInfo.path takes, because that
     * is what the plotter holds. Passing it through removePrefix would drop
     * the leg the file lives in and combine some other case's output.
     */
    it("addresses the resource path directly", () => {
      // createURL resolves against the page's own origin, which happy-dom does
      // not put on the global the way a browser does.
      vi.stubGlobal("origin", "http://localhost");

      const url = getCombinedDownloadURL(
        "/cases/engine/outputs_original/stream0/thermo.out"
      );

      expect(url).toContain(
        "/api/combine/cases/engine/outputs_original/stream0/thermo.out"
      );
    });
  });

  describe("checksum", () => {
    it("calls the correct endpoint with checksum algorithm", async () => {
      const checksumResp = { checksums: { md5: "abc123def456" } };
      globalThis.fetch = mockFetchResponse(checksumResp);

      const result = await checksum("/files/test.txt", "md5");

      expect(globalThis.fetch).toHaveBeenCalledOnce();
      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/resources/test.txt");
      expect(callUrl).toContain("checksum=md5");
      expect(result).toBe("abc123def456");
    });
  });
});
