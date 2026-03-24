import { describe, expect, it, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { useClipboardStore } from "../clipboard";

describe("clipboard store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("has correct initial state", () => {
    const store = useClipboardStore();
    expect(store.key).toBe("");
    expect(store.items).toEqual([]);
    expect(store.path).toBeUndefined();
  });

  it("allows setting clipboard key", () => {
    const store = useClipboardStore();
    store.key = "copy";
    expect(store.key).toBe("copy");
  });

  it("allows adding items to clipboard", () => {
    const store = useClipboardStore();
    store.items.push({ from: "/path/to/file.txt", name: "file.txt" });
    expect(store.items).toHaveLength(1);
    expect(store.items[0].from).toBe("/path/to/file.txt");
    expect(store.items[0].name).toBe("file.txt");
  });

  it("allows setting clipboard path", () => {
    const store = useClipboardStore();
    store.path = "/some/path";
    expect(store.path).toBe("/some/path");
  });

  it("resets state with resetClipboard", () => {
    const store = useClipboardStore();
    store.key = "copy";
    store.items.push({ from: "/a.txt", name: "a.txt" });
    store.path = "/test";

    store.resetClipboard();

    expect(store.key).toBe("");
    expect(store.items).toEqual([]);
    expect(store.path).toBeUndefined();
  });
});
