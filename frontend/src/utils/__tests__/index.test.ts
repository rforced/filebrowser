import { describe, expect, it } from "vitest";
import { reactive, ref } from "vue";
import { base64url, deepClone } from "../index";

describe("base64url", () => {
  // Expected values are standard base64url (RFC 4648 §5) of the UTF-8 bytes,
  // matching what js-base64's Base64.encodeURI produced before it was dropped.
  it.each([
    ["", ""],
    ["a", "YQ"],
    ["ab", "YWI"],
    ["abc", "YWJj"],
    ["a b/c+d.txt", "YSBiL2MrZC50eHQ"],
  ])("encodes %o", (input, expected) => {
    expect(base64url(input)).toBe(expected);
  });

  it("encodes characters outside Latin-1, which plain btoa rejects", () => {
    expect(base64url("café")).toBe("Y2Fmw6k");
    expect(base64url("日本語.txt")).toBe("5pel5pys6KqeLnR4dA");
  });

  it("uses the URL-safe alphabet and drops padding", () => {
    // "ÿþ" base64-encodes to "w7/Dvg==" in the standard alphabet.
    expect(base64url("ÿþ")).toBe("w7_Dvg");
  });

  it("is stable and collision-free across filenames, so it works as a list key", () => {
    const names = ["a.txt", "b.txt", "A.txt", "a.txt "];
    const keys = names.map(base64url);

    expect(new Set(keys).size).toBe(names.length);
    expect(names.map(base64url)).toEqual(keys);
  });
});

describe("deepClone", () => {
  it("returns primitives unchanged", () => {
    expect(deepClone(1)).toBe(1);
    expect(deepClone("x")).toBe("x");
    expect(deepClone(null)).toBe(null);
    expect(deepClone(undefined)).toBe(undefined);
  });

  it("detaches nested objects and arrays from the source", () => {
    const source = { perm: { admin: true }, rules: [{ path: "/a" }] };
    const clone = deepClone(source);

    expect(clone).toEqual(source);
    expect(clone.perm).not.toBe(source.perm);
    expect(clone.rules[0]).not.toBe(source.rules[0]);

    clone.perm.admin = false;
    expect(source.perm.admin).toBe(true);
  });

  it("keeps explicit undefined values, unlike a JSON round-trip", () => {
    const clone = deepClone({ sorting: undefined });

    expect("sorting" in clone).toBe(true);
    expect(clone.sorting).toBe(undefined);
  });

  // structuredClone throws DataCloneError on these, which is why deepClone exists.
  it("clones Vue reactive proxies", () => {
    const source = reactive({ id: 1, perm: { admin: true } });
    const clone = deepClone(source);

    expect(clone).toEqual({ id: 1, perm: { admin: true } });
    expect(clone.perm).not.toBe(source.perm);
  });

  it("clones a plain object spread from a reactive one, whose values are still proxies", () => {
    const store = reactive({ id: 1, perm: { admin: true } });
    const clone = deepClone({ ...store, locale: "en" });

    expect(clone).toEqual({ id: 1, perm: { admin: true }, locale: "en" });
  });

  it("clones the value of a ref", () => {
    const form = ref({ id: 2, rules: [{ path: "/a" }] });
    const clone = deepClone(form.value);

    expect(clone).toEqual({ id: 2, rules: [{ path: "/a" }] });
    expect(clone.rules).not.toBe(form.value.rules);
  });
});
