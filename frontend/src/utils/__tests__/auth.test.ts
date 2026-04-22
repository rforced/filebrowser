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
  signup: false,
  version: "0.0.0",
  noAuth: false,
  authMethod: "password",
  logoutPage: "/login",
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

const { routerPush } = vi.hoisted(() => ({ routerPush: vi.fn() }));
vi.mock("@/router", () => ({
  default: { push: routerPush },
}));

import { logout, saveToken } from "../auth";
import { useAuthStore } from "@/stores/auth";

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
