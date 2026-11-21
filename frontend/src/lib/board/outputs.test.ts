import assert from "node:assert/strict";
import { test } from "node:test";
import { activeOutputs, unseen } from "./outputs.ts";
import type { Board } from "../types";

const board = (over: { status?: string; seen?: number; ostatus?: string; task?: string | null } = {}) =>
  ({
    goals: [{ id: "g1", status: over.status ?? "active" }, { id: "g2", status: "done" }],
    tasks: [{ id: "t1", goal_id: "g1" }, { id: "t2", goal_id: "g2" }],
    outputs: [
      { id: "o1", task_id: over.task === undefined ? "t1" : over.task, status: over.ostatus ?? "ready", versions: [1] },
      { id: "o2", task_id: "t2", status: "review", versions: [1] },
    ],
    seen: { o1: over.seen ?? 0 },
  }) as unknown as Board;

test("keeps outputs of active goals only", () => {
  assert.deepEqual(activeOutputs(board()).map((o) => o.id), ["o1"]);
});

test("drops outputs of a done goal", () => {
  assert.deepEqual(activeOutputs(board({ status: "done" })), []);
});

test("drops outputs with no task", () => {
  assert.deepEqual(activeOutputs(board({ task: null })), []);
});

test("unseen counts new or needs-review outputs from active goals", () => {
  assert.equal(unseen(board()).length, 1);
  assert.equal(unseen(board({ seen: 1 })).length, 0);
  assert.equal(unseen(board({ seen: 1, ostatus: "review" })).length, 1);
  assert.equal(unseen(board({ status: "done" })).length, 0);
});
