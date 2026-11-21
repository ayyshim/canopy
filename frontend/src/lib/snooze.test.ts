import assert from "node:assert/strict";
import { test } from "node:test";
import { activeSnooze, pausedLabel, SNOOZE_CHOICES, tomorrowAt8 } from "./snooze.ts";

const at = (y: number, m: number, d: number, h: number, min = 0) => new Date(y, m - 1, d, h, min);

test("tomorrow means 8:00 the next morning", () => {
  const t = tomorrowAt8(at(2026, 3, 9, 22, 15));
  assert.deepEqual([t.getDate(), t.getHours(), t.getMinutes()], [10, 8, 0]);
  const early = tomorrowAt8(at(2026, 3, 9, 6, 0)); // still the next day, not this morning
  assert.equal(early.getDate(), 10);
});

test("one hour from now", () => {
  const now = at(2026, 3, 9, 10);
  assert.equal(SNOOZE_CHOICES[0].until(now).getTime() - now.getTime(), 3600_000);
});

test("a pause counts only while it is in the future", () => {
  const now = at(2026, 3, 9, 10);
  assert.equal(activeSnooze(null, now), null);
  assert.equal(activeSnooze(at(2026, 3, 9, 9).toISOString(), now), null);
  assert.ok(activeSnooze(at(2026, 3, 9, 11).toISOString(), now));
});

test("labels say today, tomorrow or the weekday", () => {
  const now = at(2026, 3, 9, 10);
  assert.match(pausedLabel(at(2026, 3, 9, 11, 30), now), /^Paused until 11:30/);
  assert.match(pausedLabel(at(2026, 3, 10, 8), now), /^Paused until tomorrow 8:00/);
  assert.match(pausedLabel(at(2026, 3, 12, 8), now), /^Paused until Thursday 8:00/);
});
