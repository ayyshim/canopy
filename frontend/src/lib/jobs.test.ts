import assert from "node:assert/strict";
import { test } from "node:test";
import { DEFAULT_PICK, durationText, formatWhen, hasRunning, jobStatus, pickText, runStatus, tokensText, type Job, type JobRun } from "./jobs.ts";

test("the picker writes text the hub understands", () => {
  assert.equal(pickText(DEFAULT_PICK), "every day at 9:00");
  assert.equal(pickText({ ...DEFAULT_PICK, freq: "weekdays", time: "09:30" }), "weekdays at 9:30");
  assert.equal(pickText({ ...DEFAULT_PICK, freq: "week", weekday: 1, time: "08:00" }), "every Monday at 8:00");
  assert.equal(pickText({ ...DEFAULT_PICK, freq: "month", monthday: 1, time: "10:00" }), "first of the month at 10:00");
  assert.equal(pickText({ ...DEFAULT_PICK, freq: "month", monthday: 22, time: "10:05" }), "22nd of the month at 10:05");
  assert.equal(pickText({ ...DEFAULT_PICK, freq: "month", monthday: 3, time: "10:05" }), "3rd of the month at 10:05");
});

const run = (o: Partial<JobRun>): JobRun => ({ id: "r", job_id: "j", number: 1, scheduled_for: "2026-03-09T03:15:00Z", trigger: "schedule", status: "succeeded", skip_reason: "", attempt: 1, tokens: 0, result: "ok", title: "", summary: "", error: "", ...o });

test("run chips say what happened", () => {
  assert.deepEqual(runStatus(run({})), { label: "Done", tone: "ok" });
  assert.equal(runStatus(run({ result: "attention" })).tone, "warn");
  assert.equal(runStatus(run({ result: "nothing_to_do" })).label, "Nothing to do");
  assert.equal(runStatus(run({ status: "failed", result: "" })).tone, "bad");
  assert.equal(runStatus(run({ status: "skipped", skip_reason: "too late", result: "" })).label, "Skipped: too late");
  assert.equal(runStatus(run({ status: "running", result: "" })).label, "Running");
});

test("memory is locked while a run is going", () => {
  assert.equal(hasRunning([run({}), run({ status: "running" })]), true);
  assert.equal(hasRunning([run({})]), false);
});

test("job status shows why a job paused", () => {
  const job = { status: "paused", pause_reason: "failures" } as Job;
  assert.equal(jobStatus(job).label, "Paused: 3 failed runs");
  assert.equal(jobStatus({ status: "active", pause_reason: "" } as Job).tone, "ok");
});

test("times show in the job's zone", () => {
  assert.match(formatWhen("2026-03-09T03:15:00Z", "Asia/Kathmandu"), /09:00/);
  assert.equal(formatWhen(null, "UTC"), "–");
});

test("small formatters", () => {
  assert.equal(durationText(45), "45 s");
  assert.equal(durationText(125), "2 min");
  assert.equal(durationText(null), "–");
  assert.equal(tokensText(0), "–");
  assert.equal(tokensText(12_400), "12.4k");
});
