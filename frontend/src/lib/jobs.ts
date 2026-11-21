import type { components } from "./api-types";

export type Job = components["schemas"]["Job"];
export type JobRun = components["schemas"]["JobRun"];
export type JobInput = components["schemas"]["JobInput"];
export type JobMemory = components["schemas"]["JobMemory"];
export type SchedulePreview = components["schemas"]["SchedulePreview"];

export type Tone = "ok" | "warn" | "bad" | "info" | "";

const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
export { WEEKDAYS };

/** What the When picker offers. */
export type Pick = { freq: "day" | "weekdays" | "week" | "month"; weekday: number; monthday: number; time: string };
export const DEFAULT_PICK: Pick = { freq: "day", weekday: 1, monthday: 1, time: "09:00" };

const ordinal = (n: number) => {
  const s = ["th", "st", "nd", "rd"];
  const v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
};

/** The picker as plain text the hub understands, such as "weekdays at 9:30". */
export function pickText(p: Pick): string {
  const [h, m] = p.time.split(":").map((x) => Number(x));
  const at = ` at ${h}:${String(m || 0).padStart(2, "0")}`;
  switch (p.freq) {
    case "weekdays": return "weekdays" + at;
    case "week": return `every ${WEEKDAYS[p.weekday]}` + at;
    case "month": return (p.monthday === 1 ? "first of the month" : `${ordinal(p.monthday)} of the month`) + at;
    default: return "every day" + at;
  }
}

/** A run time in the job's own zone, such as "Mon, Mar 9, 9:00". */
export function formatWhen(iso: string | null | undefined, zone: string): string {
  if (!iso) return "–";
  try {
    return new Intl.DateTimeFormat("en", { timeZone: zone, weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit", hour12: false }).format(new Date(iso));
  } catch {
    return new Date(iso).toLocaleString();
  }
}

export function jobStatus(j: Job): { label: string; tone: Tone } {
  if (j.status === "ended") return { label: "Ended", tone: "" };
  if (j.status === "paused") {
    const why: Record<string, string> = { failures: "Paused: 3 failed runs", cap: "Paused: token cap", owner_left: "Paused: owner left" };
    return { label: why[j.pause_reason] ?? "Paused", tone: j.pause_reason ? "warn" : "" };
  }
  return { label: "Active", tone: "ok" };
}

const RESULT: Record<string, string> = { ok: "Done", attention: "Needs a look", nothing_to_do: "Nothing to do" };

export function runStatus(r: JobRun): { label: string; tone: Tone } {
  switch (r.status) {
    case "running": return { label: "Running", tone: "info" };
    case "succeeded": return { label: RESULT[r.result] ?? "Done", tone: r.result === "attention" ? "warn" : "ok" };
    case "failed": return { label: "Failed", tone: "bad" };
    case "capped": return { label: "Hit token cap", tone: "warn" };
    case "cancelled": return { label: "Cancelled", tone: "" };
    default: return { label: r.skip_reason ? `Skipped: ${r.skip_reason}` : "Skipped", tone: "" };
  }
}

export const TRIGGER_LABEL: Record<string, string> = { schedule: "Scheduled", manual: "Run now", test: "Test run" };

export function runSeconds(r: JobRun): number | null {
  if (!r.started_at || !r.finished_at) return null;
  return Math.max(0, Math.round((Date.parse(r.finished_at) - Date.parse(r.started_at)) / 1000));
}

export function durationText(s: number | null): string {
  if (s == null) return "–";
  if (s < 60) return `${s} s`;
  if (s < 3600) return `${Math.round(s / 60)} min`;
  return `${Math.floor(s / 3600)} h ${Math.round((s % 3600) / 60)} min`;
}

export function tokensText(n: number): string {
  if (!n) return "–";
  return n >= 1000 ? `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k` : String(n);
}

/** A run is going, so memory can't be edited. */
export const hasRunning = (runs: JobRun[]) => runs.some((r) => r.status === "running");

/** Browser zone name, for a sensible default. */
export function browserZone(): string {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch { return "UTC"; }
}
