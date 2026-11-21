import assert from "node:assert/strict";
import { test } from "node:test";
import { applyUsageWork, keepUsage } from "./board/usageEvents.ts";

test("task events without usage keep the totals already shown", () => {
  const old: { id: string; usage?: { cost_micros: number } }[] = [{ id: "t1", usage: { cost_micros: 5 } }];
  assert.deepEqual(keepUsage({ id: "t1" }, old), { id: "t1", usage: { cost_micros: 5 } });
  assert.deepEqual(keepUsage({ id: "t1", usage: { cost_micros: 9 } }, old).usage, { cost_micros: 9 });
  assert.deepEqual(keepUsage({ id: "t2" }, old), { id: "t2" });
});

test("usage.work updates only the task and goal it names", () => {
  const tasks = [{ id: "t1", usage: 1 }, { id: "t2", usage: 2 }];
  const goals = [{ id: "g1", usage: 1 }];
  const r = applyUsageWork(tasks, goals, { task_id: "t1", task: 10, goal_id: "g1", goal: 20 });
  assert.deepEqual(r.tasks.map((t) => t.usage), [10, 2]);
  assert.equal(r.goals[0].usage, 20);
  // a task with no goal leaves goals alone
  assert.equal(applyUsageWork(tasks, goals, { task_id: "t2", task: 7 }).goals[0].usage, 1);
});
