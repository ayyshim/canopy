import assert from "node:assert/strict";
import { test } from "node:test";
import { fmtCost, fmtTokens } from "./usageFormat.ts";

test("tokens use k and M", () => {
  assert.equal(fmtTokens(950), "950");
  assert.equal(fmtTokens(12400), "12.4k");
  assert.equal(fmtTokens(1_200_000), "1.2M");
});

test("cost shows a dash, under a cent, or dollars", () => {
  assert.equal(fmtCost(0), "–");
  assert.equal(fmtCost(900), "<$0.01");
  assert.equal(fmtCost(310_000), "$0.31");
  assert.equal(fmtCost(12_400_000), "$12.40");
});
