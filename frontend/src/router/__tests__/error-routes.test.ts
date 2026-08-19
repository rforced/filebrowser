import { describe, expect, it, vi } from "vitest";

// The route table is the subject, so the views behind it are stubbed away —
// this file would otherwise drag i18n, the stores and every view into the
// environment, which is exactly what catchAll.ts was split out to avoid.
// vi.mock is hoisted above every binding in this file, so each factory has to
// stand alone rather than share a helper.
vi.mock("@/views/Login.vue", () => ({ default: { name: "Login" } }));
vi.mock("@/views/Layout.vue", () => ({ default: { name: "Layout" } }));
vi.mock("@/views/Files.vue", () => ({ default: { name: "Files" } }));
vi.mock("@/views/Share.vue", () => ({ default: { name: "Share" } }));
vi.mock("@/views/Settings.vue", () => ({ default: { name: "Settings" } }));
vi.mock("@/views/settings/Users.vue", () => ({ default: { name: "Users" } }));
vi.mock("@/views/settings/User.vue", () => ({ default: { name: "User" } }));
vi.mock("@/views/settings/Global.vue", () => ({ default: { name: "Global" } }));
vi.mock("@/views/settings/Profile.vue", () => ({
  default: { name: "Profile" },
}));
vi.mock("@/views/settings/Shares.vue", () => ({ default: { name: "Shares" } }));
vi.mock("@/views/Errors.vue", () => ({ default: { name: "Errors" } }));

vi.mock("@/utils/constants", () => ({ baseURL: "/", name: "Test" }));
vi.mock("@/i18n", () => ({ default: { global: { t: (k: string) => k } } }));
vi.mock("@/utils/auth", () => ({ validateLogin: vi.fn() }));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({ isLoggedIn: false }),
}));

import { router } from "../index";

describe("the error routes", () => {
  // Reached on their own they used to be a card on a blank tab: no header, no
  // footer, nowhere to go. /403 is the live case — the admin guard sends a
  // non-admin there from /settings/users.
  it.each([
    ["/403", "Forbidden"],
    ["/404", "NotFound"],
    ["/500", "InternalServerError"],
  ])("renders %s inside the app frame", (path, name) => {
    const resolved = router.resolve(path);

    expect(resolved.name).toBe(name);
    expect(
      resolved.matched.map((r) => r.components?.default),
      "the error page should sit inside a Layout parent"
    ).toHaveLength(2);
    expect((resolved.matched[0].components?.default as any).name).toBe(
      "Layout"
    );
    expect((resolved.matched[1].components?.default as any).name).toBe(
      "Errors"
    );
  });

  // Guards the shape of the fix: giving the three a single parent at "/" would
  // match the bare root URL with no child and render an empty layout there,
  // taking this redirect out of play.
  it("leaves the bare root url to the catch-all redirect", () => {
    const resolved = router.resolve("/");

    expect(
      resolved.matched.some((r) => r.components?.default),
      "/ must not land on a route that renders anything of its own"
    ).toBe(false);
    expect(router.resolve("/").fullPath).toBe("/");
    expect(resolved.matched[0]?.redirect).toBeDefined();
  });

  it("still routes /files and /share through the layout", () => {
    expect(router.resolve("/files/case/").name).toBe("Files");
    expect(router.resolve("/share/ABC123/").name).toBe("Share");
  });
});
