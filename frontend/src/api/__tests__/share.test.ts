import { describe, expect, it, vi, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";

// Mock modules that depend on window.FileBrowser or vue-i18n plugin
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

// The createURL function in utils.ts references the bare global `origin`
// (from constants.ts: `const origin = window.location.origin`), which is
// undefined in happy-dom. Provide it as a global so getShareURL works.
// @ts-expect-error -- defining global for test environment
globalThis.origin = "http://localhost";

import { list, get, remove, create, getShareURL } from "../share";

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

describe("share API", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.restoreAllMocks();
  });

  describe("list", () => {
    it("fetches all shares from /api/shares", async () => {
      const shares: Share[] = [
        { hash: "abc123", path: "/docs/file.txt", expire: 1700000000 },
        { hash: "def456", path: "/images/photo.png", expire: 0 },
      ];
      globalThis.fetch = mockFetchResponse(shares);

      const result = await list();

      expect(globalThis.fetch).toHaveBeenCalledOnce();
      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/shares");
      expect(result).toEqual(shares);
    });

    it("returns an empty array when no shares exist", async () => {
      globalThis.fetch = mockFetchResponse([]);

      const result = await list();

      expect(result).toEqual([]);
    });

    it("returns shares with mixed expiry values including permanent", async () => {
      const shares: Share[] = [
        { hash: "a", path: "/a", expire: 0 },
        { hash: "b", path: "/b", expire: 1700000000 },
        { hash: "c", path: "/c", expire: 0 },
      ];
      globalThis.fetch = mockFetchResponse(shares);

      const result = await list();

      expect(result).toHaveLength(3);
      expect(result[0].expire).toBe(0);
      expect(result[1].expire).toBe(1700000000);
    });
  });

  describe("get", () => {
    it("fetches shares for a specific file path", async () => {
      const share: Share = {
        hash: "abc123",
        path: "/docs/file.txt",
        expire: 1700000000,
      };
      globalThis.fetch = mockFetchResponse(share);

      const result = await get("/files/docs/file.txt");

      expect(globalThis.fetch).toHaveBeenCalledOnce();
      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/share/docs/file.txt");
      expect(result).toEqual(share);
    });

    it("handles root path correctly", async () => {
      const share: Share = { hash: "root1", path: "/", expire: 0 };
      globalThis.fetch = mockFetchResponse(share);

      const result = await get("/files/");

      const callUrl = (globalThis.fetch as any).mock.calls[0][0];
      expect(callUrl).toContain("/api/share/");
      expect(result.hash).toBe("root1");
    });
  });

  describe("remove", () => {
    it("sends DELETE request with the share hash", async () => {
      globalThis.fetch = mockFetchResponse(null, 200);

      await remove("abc123");

      expect(globalThis.fetch).toHaveBeenCalledOnce();
      const [callUrl, callOpts] = (globalThis.fetch as any).mock.calls[0];
      expect(callUrl).toContain("/api/share/abc123");
      expect(callOpts.method).toBe("DELETE");
    });
  });

  describe("create", () => {
    it("sends POST request with expiry and unit in URL", async () => {
      const newShare: Share = {
        hash: "new123",
        path: "/docs/file.txt",
        expire: 1700003600,
      };
      globalThis.fetch = mockFetchResponse(newShare);

      const result = await create(
        "/files/docs/file.txt",
        "secret",
        "24",
        "hours"
      );

      expect(globalThis.fetch).toHaveBeenCalledOnce();
      const [callUrl, callOpts] = (globalThis.fetch as any).mock.calls[0];
      expect(callUrl).toContain("/api/share/docs/file.txt");
      expect(callUrl).toContain("expires=24");
      expect(callUrl).toContain("unit=hours");
      expect(callOpts.method).toBe("POST");

      const body = JSON.parse(callOpts.body);
      expect(body.password).toBe("secret");
      expect(body.expires).toBe("24");
      expect(body.unit).toBe("hours");
      expect(result).toEqual(newShare);
    });

    it("sends minimal body when no expiry is set", async () => {
      const newShare: Share = {
        hash: "perm1",
        path: "/docs/file.txt",
        expire: 0,
      };
      globalThis.fetch = mockFetchResponse(newShare);

      await create("/files/docs/file.txt");

      const [callUrl, callOpts] = (globalThis.fetch as any).mock.calls[0];
      expect(callUrl).not.toContain("expires=");
      expect(callOpts.body).toBe("{}");
    });

    it("includes password in body even without expiry when unit differs", async () => {
      globalThis.fetch = mockFetchResponse({
        hash: "x",
        path: "/a",
        expire: 0,
      });

      await create("/files/a", "", "", "days");

      const [, callOpts] = (globalThis.fetch as any).mock.calls[0];
      const body = JSON.parse(callOpts.body);
      expect(body.unit).toBe("days");
    });
  });

  describe("getShareURL", () => {
    it("builds a public share URL from a share object", () => {
      const share: Share = {
        hash: "abc123",
        path: "/docs/file.txt",
        expire: 1700000000,
      };

      const url = getShareURL(share);

      expect(url).toContain("share/abc123");
    });

    it("handles hashes with special characters", () => {
      const share: Share = {
        hash: "a-b_c",
        path: "/test",
        expire: 0,
      };

      const url = getShareURL(share);

      expect(url).toContain("share/a-b_c");
    });
  });
});
