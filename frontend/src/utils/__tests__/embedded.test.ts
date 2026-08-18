import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

vi.mock("@/utils/constants", () => ({
  baseURL: "",
  authMethod: "hook",
  domain: "https://horizon.test",
}));

const { saveTokenMock } = vi.hoisted(() => ({ saveTokenMock: vi.fn() }));
vi.mock("@/utils/auth", () => ({ saveToken: saveTokenMock }));

const PARENT_ORIGIN = "https://horizon.test";

function stubEmbeddedWindow() {
  const parentStub = { postMessage: vi.fn() };
  vi.stubGlobal("top", { name: "embedding-page" });
  vi.stubGlobal("parent", parentStub);
  return parentStub;
}

function deliverCode(origin: string, data: unknown) {
  window.dispatchEvent(
    new MessageEvent("message", {
      origin,
      source: window.parent as unknown as MessageEventSource,
      data,
    })
  );
}

async function importEmbedded() {
  return await import("../embedded");
}

describe("embeddedHandoff", () => {
  beforeEach(() => {
    vi.resetModules();
    saveTokenMock.mockReset();
    globalThis.fetch = vi.fn();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("resolves false outside a frame without talking to anyone", async () => {
    const { embeddedHandoff } = await importEmbedded();

    await expect(embeddedHandoff()).resolves.toBe(false);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("treats ?embed as embedded styling without enabling the handoff", async () => {
    window.history.replaceState(null, "", "/?embed=1");
    try {
      const mod = await importEmbedded();

      expect(mod.framed).toBe(false);
      expect(mod.embedded).toBe(true);
      await expect(mod.embeddedHandoff()).resolves.toBe(false);
    } finally {
      window.history.replaceState(null, "", "/");
    }
  });

  it("exchanges a parent-delivered code for a session", async () => {
    const parentStub = stubEmbeddedWindow();
    globalThis.fetch = vi.fn().mockResolvedValue({
      status: 200,
      text: () => Promise.resolve("session-token"),
    });

    const { embeddedHandoff } = await importEmbedded();

    const exchange = embeddedHandoff();
    expect(parentStub.postMessage).toHaveBeenCalledWith(
      { type: "horizon:fm-handoff-request" },
      PARENT_ORIGIN
    );

    deliverCode(PARENT_ORIGIN, {
      type: "horizon:fm-handoff-code",
      code: "one-time-code",
    });

    await expect(exchange).resolves.toBe(true);
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/handoff",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ code: "one-time-code" }),
      })
    );
    expect(saveTokenMock).toHaveBeenCalledWith("session-token");
  });

  it("ignores a code claiming to come from anywhere but the platform", async () => {
    stubEmbeddedWindow();
    vi.useFakeTimers();

    const { embeddedHandoff } = await importEmbedded();

    const exchange = embeddedHandoff();
    deliverCode("https://evil.example", {
      type: "horizon:fm-handoff-code",
      code: "planted-code",
    });

    await vi.advanceTimersByTimeAsync(5000);

    await expect(exchange).resolves.toBe(false);
    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(saveTokenMock).not.toHaveBeenCalled();
  });

  it("asks the platform for the theme and follows every push", async () => {
    const parentStub = stubEmbeddedWindow();
    document.documentElement.classList.remove("dark");

    const { syncEmbeddedTheme } = await importEmbedded();

    syncEmbeddedTheme();
    expect(parentStub.postMessage).toHaveBeenCalledWith(
      { type: "horizon:fm-theme-request" },
      PARENT_ORIGIN
    );

    deliverCode(PARENT_ORIGIN, { type: "horizon:fm-theme", theme: "dark" });
    expect(document.documentElement.classList.contains("dark")).toBe(true);

    deliverCode(PARENT_ORIGIN, { type: "horizon:fm-theme", theme: "light" });
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("ignores a theme claiming to come from anywhere but the platform", async () => {
    stubEmbeddedWindow();
    document.documentElement.classList.remove("dark");

    const { syncEmbeddedTheme } = await importEmbedded();

    syncEmbeddedTheme();
    deliverCode("https://evil.example", {
      type: "horizon:fm-theme",
      theme: "dark",
    });

    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("reports a refused exchange without storing anything", async () => {
    stubEmbeddedWindow();
    globalThis.fetch = vi.fn().mockResolvedValue({
      status: 403,
      text: () => Promise.resolve(""),
    });

    const { embeddedHandoff } = await importEmbedded();

    const exchange = embeddedHandoff();
    deliverCode(PARENT_ORIGIN, {
      type: "horizon:fm-handoff-code",
      code: "spent-code",
    });

    await expect(exchange).resolves.toBe(false);
    expect(saveTokenMock).not.toHaveBeenCalled();
  });
});
