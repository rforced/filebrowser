import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
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
  logoutPage: "/login",
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

const { routerPush } = vi.hoisted(() => ({ routerPush: vi.fn() }));
vi.mock("@/router", () => ({
  default: { push: routerPush },
}));

import { login, logout, saveToken } from "../auth";
import { useAuthStore } from "@/stores/auth";
import { StatusError } from "@/api/utils";

function mockMeOk(user: Partial<IUser> = { locale: "en" }) {
  return vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    statusText: "OK",
    headers: new Headers(),
    json: () => Promise.resolve(user),
    text: () => Promise.resolve(JSON.stringify(user)),
  });
}

describe("logout(reason)", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    routerPush.mockClear();
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: "OK",
      headers: new Headers(),
      text: () => Promise.resolve(""),
    });
    localStorage.clear();
  });

  it("redirects to /login with ?logout-reason=session_expired so the banner renders", async () => {
    await logout("session_expired");

    expect(routerPush).toHaveBeenCalledOnce();
    expect(routerPush).toHaveBeenCalledWith({
      path: "/login",
      query: { "logout-reason": "session_expired" },
    });
  });

  it("redirects to /login with no query when no reason is provided", async () => {
    await logout();

    expect(routerPush).toHaveBeenCalledWith({ path: "/login" });
  });

  it("treats an empty/whitespace reason as no reason (no query set)", async () => {
    await logout("   ");

    expect(routerPush).toHaveBeenCalledWith({ path: "/login" });
  });

  it("calls POST /api/logout with the stored X-Auth token so the server-side token is deleted", async () => {
    const authStore = useAuthStore();
    authStore.token = "stored-token-abc";

    await logout("session_expired");

    expect(globalThis.fetch).toHaveBeenCalledOnce();
    const [url, opts] = (globalThis.fetch as any).mock.calls[0];
    expect(url).toBe("/test/api/logout");
    expect(opts.method).toBe("POST");
    expect(opts.headers["X-Auth"]).toBe("stored-token-abc");
  });

  it("skips the /api/logout call when there is no stored token", async () => {
    await logout("session_expired");

    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("clears the auth store and local storage token on logout", async () => {
    const authStore = useAuthStore();
    authStore.token = "stored-token-abc";
    authStore.user = { username: "alice" } as IUser;
    localStorage.setItem("token", "stored-token-abc");

    await logout("session_expired");

    expect(authStore.token).toBe("");
    expect(authStore.user).toBeNull();
    expect(localStorage.getItem("token")).toBe("");
  });
});

describe("login", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
  });

  function mockLoginResponse(status: number, body: string) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      statusText: "",
      headers: new Headers(),
      text: () => Promise.resolve(body),
      json: () => Promise.resolve({ locale: "en" }),
    });
  }

  it("sends the MFA code alongside the credentials", async () => {
    mockLoginResponse(200, "issued-token");

    await login("alice", "secret", "captcha-token", "123456");

    const [url, opts] = (globalThis.fetch as any).mock.calls[0];
    expect(url).toBe("/test/api/login");
    expect(JSON.parse(opts.body)).toEqual({
      username: "alice",
      password: "secret",
      recaptcha: "captcha-token",
      mfaCode: "123456",
    });
  });

  it("defaults the MFA code to empty when the user has not been asked for one", async () => {
    mockLoginResponse(200, "issued-token");

    await login("alice", "secret", "captcha-token");

    const [, opts] = (globalThis.fetch as any).mock.calls[0];
    expect(JSON.parse(opts.body).mfaCode).toBe("");
  });

  // The login page distinguishes "needs a second factor" from "wrong password"
  // by the structured code, so it must survive the throw.
  it("surfaces the MFA challenge code and method from a 401", async () => {
    mockLoginResponse(
      401,
      JSON.stringify({
        code: "mfaRequired",
        message: "multi-factor authentication required",
        params: { method: "email" },
      })
    );

    const err = await login("alice", "secret", "captcha-token").catch((e) => e);

    expect(err).toBeInstanceOf(StatusError);
    expect(err.status).toBe(401);
    expect(err.code).toBe("mfaRequired");
    expect(err.params).toEqual({ method: "email" });
  });

  it("surfaces a rejected code as mfaInvalid rather than a generic failure", async () => {
    mockLoginResponse(
      401,
      JSON.stringify({
        code: "mfaInvalid",
        message: "invalid multi-factor authentication code",
        params: { method: "totp" },
      })
    );

    const err = await login("alice", "secret", "captcha-token", "000000").catch(
      (e) => e
    );

    expect(err.code).toBe("mfaInvalid");
    expect(err.params).toEqual({ method: "totp" });
  });

  it("leaves the code unset for a plain rejection, so it reads as wrong credentials", async () => {
    mockLoginResponse(403, "");

    const err = await login("alice", "wrong", "captcha-token").catch((e) => e);

    expect(err.status).toBe(403);
    expect(err.code).toBeUndefined();
  });
});

describe("saveToken", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
    localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not schedule any client-side idle/logout timer (session validity is server-governed)", async () => {
    globalThis.fetch = mockMeOk({ locale: "en" });
    const setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");

    await saveToken("some-opaque-token");

    // Regression guard: the previous implementation set a 2h timer that
    // forcibly logged users out mid-upload. That behavior has been removed.
    expect(setTimeoutSpy).not.toHaveBeenCalled();
    // And advancing time past the old 2h threshold must not trigger logout.
    vi.advanceTimersByTime(3 * 60 * 60 * 1000);
    expect(routerPush).not.toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({ "logout-reason": "inactivity" }),
      })
    );
  });

  it("persists the token to localStorage and the auth store", async () => {
    globalThis.fetch = mockMeOk({ locale: "en" });

    await saveToken("some-opaque-token");

    const authStore = useAuthStore();
    expect(authStore.token).toBe("some-opaque-token");
    expect(localStorage.getItem("token")).toBe("some-opaque-token");
  });
});
