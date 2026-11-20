import assert from "node:assert/strict";
import { test } from "node:test";
import { isWorkspaceActive, liveIconSvg, tabTitle } from "./liveStatus.ts";

test("title format", () => {
  assert.equal(tabTitle("Acme", "Web", false), "Acme > Web");
  assert.equal(tabTitle(undefined, "Web", false), "Web");
});

test("title gains LIVE when active", () => {
  assert.equal(tabTitle("Acme", "Web", true), "Acme > Web · ● LIVE");
});

test("active/idle detection", () => {
  assert.equal(isWorkspaceActive({ tasks: [], goals: [] }), false);
  assert.equal(isWorkspaceActive({ tasks: [{ status: "todo" }, { status: "done" }], goals: [{ status: "done" }] }), false);
  assert.equal(isWorkspaceActive({ tasks: [{ status: "doing" }], goals: [] }), true);
  assert.equal(isWorkspaceActive({ tasks: [], goals: [{ status: "active" }] }), true);
});

test("icon has the green dot only when live", () => {
  assert.ok(liveIconSvg(true).includes("#22C55E"));
  assert.ok(!liveIconSvg(false).includes("#22C55E"));
});
