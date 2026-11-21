import assert from "node:assert/strict";
import { test } from "node:test";
import { tasksHiddenByGoal } from "./taskShown.ts";

test("a done goal hides its tasks until they are shown", () => {
  assert.equal(tasksHiddenByGoal({ status: "done", show_tasks: false }), true);
  assert.equal(tasksHiddenByGoal({ status: "done", show_tasks: true }), false);
});

test("open goals and tasks without a goal are never hidden", () => {
  for (const status of ["draft", "planning", "active", "ready"]) assert.equal(tasksHiddenByGoal({ status, show_tasks: false }), false);
  assert.equal(tasksHiddenByGoal(undefined), false);
});

test("a stopped goal hides its tasks like a done one", () => {
  assert.equal(tasksHiddenByGoal({ status: "stopped", show_tasks: false }), true);
});
