import { describe, expect, it, vi } from "vitest";

vi.mock("@/api", () => ({ files: { fetch: vi.fn() } }));
vi.mock("@/api/utils", () => ({ createURL: vi.fn() }));
vi.mock("@/stores/auth", () => ({ useAuthStore: vi.fn() }));
vi.mock("@/utils/convergeSummaryCache", () => ({
  cachedConvergeSummary: vi.fn(),
}));

import { selectLegsWithinBudget, type ChainLeg } from "@/utils/outChain";

const leg = (name: string, size: number): ChainLeg => ({
  runName: name,
  runPath: `/case/${name}`,
  filePath: `/case/${name}/stream0/dynamic.out`,
  size,
  current: false,
});

describe("selectLegsWithinBudget", () => {
  it("keeps every leg when the total fits", () => {
    const legs = [leg("outputs_original", 10), leg("outputs_restart1", 20)];
    expect(selectLegsWithinBudget(legs, 30)).toEqual(legs);
  });

  it("drops the oldest legs first", () => {
    const legs = [
      leg("outputs_original", 30),
      leg("outputs_restart1", 30),
      leg("outputs_restart2", 30),
    ];
    expect(selectLegsWithinBudget(legs, 70).map((l) => l.runName)).toEqual([
      "outputs_restart1",
      "outputs_restart2",
    ]);
  });

  it("drops legs that alone exceed the budget wherever they sit", () => {
    const legs = [
      leg("outputs_original", 10),
      leg("outputs_restart1", 100),
      leg("outputs_restart2", 10),
    ];
    expect(selectLegsWithinBudget(legs, 50).map((l) => l.runName)).toEqual([
      "outputs_original",
      "outputs_restart2",
    ]);
  });

  it("always keeps the newest fitting leg", () => {
    const legs = [leg("outputs_original", 40), leg("outputs_restart1", 40)];
    expect(selectLegsWithinBudget(legs, 40).map((l) => l.runName)).toEqual([
      "outputs_restart1",
    ]);
  });

  it("returns nothing when no leg fits", () => {
    expect(selectLegsWithinBudget([leg("outputs_original", 99)], 50)).toEqual(
      []
    );
  });
});
