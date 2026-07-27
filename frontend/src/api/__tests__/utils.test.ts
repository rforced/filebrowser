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

import { fetchURL, StatusError } from "../utils";
import { logout, renew } from "@/utils/auth";

function mockResponse({
  status,
  body = "",
  headers = {},
}: {
  status: number;
  body?: string;
  headers?: Record<string, string>;
}) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: "STATUS",
    headers: new Headers(headers),
    json: () => Promise.resolve(body ? JSON.parse(body) : {}),
    text: () => Promise.resolve(body),
  });
}

describe("fetchURL", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });

  describe("when the server reports an expired session", () => {
    it("calls logout('session_expired') on 401 so the login view can render the session-expired banner", async () => {
      globalThis.fetch = mockResponse({ status: 401, body: "" });

      await expect(fetchURL("/api/resources/", {})).rejects.toBeInstanceOf(
        StatusError
      );

      expect(logout).toHaveBeenCalledOnce();
      expect(logout).toHaveBeenCalledWith("session_expired");
    });

    it("throws a StatusError with status 401 so callers can react", async () => {
      globalThis.fetch = mockResponse({ status: 401, body: "unauthorized" });

      try {
        await fetchURL("/api/resources/", {});
        throw new Error("expected rejection");
      } catch (e) {
        expect(e).toBeInstanceOf(StatusError);
        expect((e as StatusError).status).toBe(401);
      }
    });

    it("does not call logout when the 401 comes from an auth=false request", async () => {
      globalThis.fetch = mockResponse({ status: 401, body: "" });

      await expect(
        fetchURL("/api/public/", {}, /* auth */ false)
      ).rejects.toBeInstanceOf(StatusError);

      expect(logout).not.toHaveBeenCalled();
    });
  });

  describe("when the server accepts the request", () => {
    it("does not call logout on a 2xx response", async () => {
      globalThis.fetch = mockResponse({ status: 200, body: "ok" });

      await fetchURL("/api/resources/", {});

      expect(logout).not.toHaveBeenCalled();
    });

    it("does not call logout on non-401 error statuses", async () => {
      globalThis.fetch = mockResponse({ status: 500, body: "boom" });

      await expect(fetchURL("/api/resources/", {})).rejects.toBeInstanceOf(
        StatusError
      );

      expect(logout).not.toHaveBeenCalled();
    });

    it("calls renew when the server sets X-Renew-Token: true", async () => {
      globalThis.fetch = mockResponse({
        status: 200,
        body: "ok",
        headers: { "X-Renew-Token": "true" },
      });

      await fetchURL("/api/resources/", {});

      expect(renew).toHaveBeenCalledOnce();
    });
  });

  describe("when the server discloses why a request failed", () => {
    it("lifts the code and params off a structured body so callers can localize it", async () => {
      globalThis.fetch = mockResponse({
        status: 400,
        body: JSON.stringify({
          code: "passwordTooShort",
          message: "password is too short, minimum length is 12",
          params: { min: "12" },
        }),
      });

      try {
        await fetchURL("/api/share/f.txt", { method: "POST" });
        throw new Error("expected rejection");
      } catch (e) {
        const error = e as StatusError;
        expect(error.code).toBe("passwordTooShort");
        expect(error.params).toEqual({ min: "12" });
        // Callers that just display the error must not be shown raw JSON.
        expect(error.message).toBe(
          "password is too short, minimum length is 12"
        );
      }
    });

    it("leaves a plain-text failure body as the message", async () => {
      globalThis.fetch = mockResponse({ status: 400, body: "400 Bad Request" });

      try {
        await fetchURL("/api/share/f.txt", { method: "POST" });
        throw new Error("expected rejection");
      } catch (e) {
        const error = e as StatusError;
        expect(error.code).toBeUndefined();
        expect(error.message).toBe("400 Bad Request");
      }
    });

    it("keeps a JSON body that names no code as the raw message", async () => {
      const body = JSON.stringify({ something: "else" });
      globalThis.fetch = mockResponse({ status: 500, body });

      try {
        await fetchURL("/api/share/f.txt", { method: "POST" });
        throw new Error("expected rejection");
      } catch (e) {
        const error = e as StatusError;
        expect(error.code).toBeUndefined();
        expect(error.message).toBe(body);
      }
    });
  });
});
