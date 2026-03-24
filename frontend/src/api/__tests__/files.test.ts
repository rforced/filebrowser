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

vi.mock("@/utils/auth", () => ({
  renew: vi.fn(),
  logout: vi.fn(),
}));

import { dirSize, checksum } from "../files";

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
    it("calls the correct endpoint with dirsize=true", async () => {
      const dirInfo = { size: 1024, numFiles: 5, numDirs: 2 };
      globalThis.fetch = mockFetchResponse(dirInfo);

      await dirSize("/files/documents/");

      expect(globalThis.fetch).toHaveBeenCalledOnce();
      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/resources/documents/");
      expect(callUrl).toContain("dirsize=true");
    });

    it("returns DirSizeInfo with size, numFiles, and numDirs", async () => {
      const dirInfo = { size: 4096, numFiles: 10, numDirs: 3 };
      globalThis.fetch = mockFetchResponse(dirInfo);

      const result = await dirSize("/files/my-folder/");

      expect(result).toEqual(dirInfo);
      expect(result.size).toBe(4096);
      expect(result.numFiles).toBe(10);
      expect(result.numDirs).toBe(3);
    });

    it("handles an empty directory response", async () => {
      const dirInfo = { size: 0, numFiles: 0, numDirs: 0 };
      globalThis.fetch = mockFetchResponse(dirInfo);

      const result = await dirSize("/files/empty/");

      expect(result).toEqual({ size: 0, numFiles: 0, numDirs: 0 });
    });

    it("handles large directories with many files", async () => {
      const dirInfo = { size: 10737418240, numFiles: 50000, numDirs: 1200 };
      globalThis.fetch = mockFetchResponse(dirInfo);

      const result = await dirSize("/files/large-dir/");

      expect(result.size).toBe(10737418240);
      expect(result.numFiles).toBe(50000);
      expect(result.numDirs).toBe(1200);
    });

    it("sends a GET request", async () => {
      globalThis.fetch = mockFetchResponse({
        size: 0,
        numFiles: 0,
        numDirs: 0,
      });

      await dirSize("/files/test/");

      const callOpts = (globalThis.fetch as any).mock.calls[0][1];
      expect(callOpts.method).toBe("GET");
    });

    it("strips the /files prefix from the URL", async () => {
      globalThis.fetch = mockFetchResponse({
        size: 100,
        numFiles: 1,
        numDirs: 0,
      });

      await dirSize("/files/path/to/folder/");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/resources/path/to/folder/");
      expect(callUrl).not.toContain("/files/");
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

    it("supports sha256 algorithm", async () => {
      const checksumResp = { checksums: { sha256: "deadbeef" } };
      globalThis.fetch = mockFetchResponse(checksumResp);

      const result = await checksum("/files/data.bin", "sha256");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("checksum=sha256");
      expect(result).toBe("deadbeef");
    });
  });
});
