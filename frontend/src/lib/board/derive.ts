// Read-only helpers over a board snapshot. Components use these instead of digging into lists.
import type { AccountView, AnyMember, Board, Computer, GoalView, MemberView, SignalView, TaskView } from "../types";
import { isHidden } from "../types";
import { tasksHiddenByGoal } from "../taskShown";

export const isAdmin = (b: Board) => b.viewer.role === "admin";

export const member = (b: Board, id?: string | null): AnyMember | undefined => (id ? b.members.find((m) => m.id === id) : undefined);
export const visibleMember = (b: Board, id?: string | null): MemberView | undefined => {
  const m = member(b, id);
  return m && !isHidden(m) ? m : undefined;
};
export const task = (b: Board, id?: string | null) => (id ? b.tasks.find((t) => t.id === id) : undefined);
export const goal = (b: Board, id?: string | null) => (id ? b.goals.find((g) => g.id === id) : undefined);
export const directory = (b: Board, id?: string | null) => (id ? b.directories.find((d) => d.id === id) : undefined);
export const person = (b: Board, id?: string | null) => (id ? b.people.find((p) => p.id === id) : undefined);
export const personName = (b: Board, id?: string | null) => person(b, id)?.name ?? "Someone";
export const firstName = (b: Board, id?: string | null) => personName(b, id).split(" ")[0];
export const computer = (b: Board, id?: string | null): Computer | undefined => (id ? b.runners.find((r) => r.id === id) : undefined);

export const managers = (b: Board) => b.members.filter((m) => m.kind === "manager");
export const staffOf = (b: Board, managerId: string) => b.members.filter((m) => m.kind === "staff" && m.reports_to === managerId);
/** Isolated staff only talk to the person who created them. */
export const chatLocked = (m: MemberView, me: string) => m.isolated && m.created_by !== me;
export const independentStaff = (b: Board) => b.members.filter((m) => m.kind === "staff" && !m.reports_to);

export const goalTasks = (b: Board, goalId: string) => b.tasks.filter((t) => t.goal_id === goalId);
export const workTasks = (b: Board, goalId: string) => goalTasks(b, goalId).filter((t) => t.kind !== "plan" && t.kind !== "handover");

/** The latest handover.md of a goal, if a brief was written. */
export function handoverBrief(b: Board, goalId: string) {
  const ids = new Set(goalTasks(b, goalId).filter((t) => t.kind === "handover").map((t) => t.id));
  return b.outputs
    .filter((o) => o.name === "handover.md" && o.task_id && ids.has(o.task_id))
    .sort((x, y) => y.created_at.localeCompare(x.created_at))[0];
}

/** The open review of a goal's handover brief (only visible to the goal's person and admins). */
export const handoverReview = (b: Board, goalId: string) =>
  b.signals.find((s) => s.type === "review" && s.task_id && task(b, s.task_id)?.kind === "handover" && task(b, s.task_id)?.goal_id === goalId);

/** True while a brief waits for review, even for people who can't answer it. */
export const briefInReview = (b: Board, goalId: string) =>
  !!handoverReview(b, goalId) || goalTasks(b, goalId).some((t) => t.kind === "handover" && t.status === "review");
export function goalProgress(b: Board, g: GoalView): number {
  const ts = workTasks(b, g.id);
  if (!ts.length) return g.status === "done" ? 100 : 0;
  return Math.round(ts.reduce((a, t) => a + (t.status === "done" ? 100 : t.progress), 0) / ts.length);
}
export const goalRepos = (b: Board, goalId: string) => b.goal_repos.filter((r) => r.goal_id === goalId);
export const goalHasCode = (b: Board, goalId: string) => goalRepos(b, goalId).some((r) => r.commits > 0);

/** A done goal's tasks leave the board unless someone shows them again. */
export function hiddenTask(b: Board, t: TaskView): boolean {
  return tasksHiddenByGoal(goal(b, t.goal_id));
}

export const signalsForTask = (b: Board, taskId: string) => b.signals.filter((s) => s.task_id === taskId);
export const signalsForMember = (b: Board, memberId: string) => b.signals.filter((s) => s.member_id === memberId);
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const payload = (s: SignalView): Record<string, any> => (s.payload ?? {}) as Record<string, any>;

/** needsMe: the open items this viewer can act on, oldest first. */
export function needsMe(b: Board, me: string): SignalView[] {
  return b.signals.filter((s) => {
    const p = payload(s);
    switch (s.type) {
      case "owner_change":
      case "sync_conflict":
        return p.owner_user_id === me;
      case "usage_limit":
      case "cap":
      case "goal_ready":
        return s.for_user_id ? s.for_user_id === me : isAdmin(b);
      case "question":
        return !p.delegated;
    }
    return true;
  });
}

/** The computer a task runs on (DECISIONS: Where work runs). */
export function runnerForTask(b: Board, t: TaskView): string | null {
  if (t.kind === "owner_change") {
    for (const need of t.required_dirs) {
      const d = directory(b, need.directory_id);
      if (d?.machine_id) return d.machine_id;
    }
  }
  const g = goal(b, t.goal_id);
  if (g?.runner_id) return g.runner_id;
  const who = g?.created_by ?? t.attributed_user_id ?? null;
  return who ? computerFor(b, who) : null;
}

/** The computer a person's goals run on: their own, else a shared one. */
export function computerFor(b: Board, personId: string): string | null {
  const own = b.computers[personId];
  if (own && computer(b, own)) return own;
  return b.runners.find((r) => r.shared)?.id ?? null;
}

export const account = (b: Board, runnerId: string | null | undefined, cli = "claude"): AccountView | undefined =>
  runnerId ? b.accounts.find((a) => a.runner_id === runnerId && a.cli === cli) : undefined;

/** whyWaiting says why work can't run on a computer now ("" when it can). */
export function whyWaiting(b: Board, runnerId: string | null | undefined, cli = "claude"): string {
  const r = computer(b, runnerId);
  if (!r) return "No computer to run on";
  if (!r.online) return "Waiting for " + r.name;
  const a = account(b, r.id, cli);
  if (a?.paused) return "Paused at the daily cap on " + r.name;
  if (a?.limited_until && new Date(a.limited_until) > new Date()) return "Usage limit on " + r.name + ", back " + clock(a.limited_until);
  return "";
}

/** How a computer signs in, in words: "Riley’s Claude subscription", "organization API key". */
export function signInLabel(b: Board, runnerId: string | null | undefined): string {
  const r = computer(b, runnerId);
  if (!r) return "no sign-in yet";
  const a = account(b, r.id);
  const who = firstName(b, r.owner_user_id);
  if (a?.mode === "api_key") return r.shared ? "an organization API key" : `${who}’s API key`;
  return `${who}’s Claude subscription`;
}

export type Status = { k: "working" | "need" | "error" | "idle" | "asleep"; t: string };

/** memberStatus is the one-line status under a member on the board. */
export function memberStatus(b: Board, m: MemberView): Status {
  const sig = signalsForMember(b, m.id);
  if (sig.some((s) => s.type === "error")) return { k: "error", t: "Stopped on an error" };
  const live = b.tasks.filter((t) => t.member_id === m.id && (t.status === "doing" || t.status === "todo"));
  const doing = live.filter((t) => t.status === "doing");
  const halted = (doing.length ? doing : live).map((t) => whyWaiting(b, runnerForTask(b, t), m.cli)).find(Boolean);
  if (halted) return { k: "need", t: halted };
  if (sig.some((s) => s.type === "question" && !payload(s).delegated)) return { k: "need", t: "Waiting for your answer" };
  if (sig.some((s) => s.type === "gate" || s.type === "dir_access")) return { k: "need", t: "Paused at a gate" };
  if (sig.some((s) => s.type === "review")) return { k: "need", t: "Waiting for your review" };
  const sessions = b.sessions.filter((s) => s.member_id === m.id && s.status !== "ended");
  if (sessions.some((s) => s.status === "crashed")) return { k: "error", t: "Crashed, restarting" };
  if (doing.length) {
    const n = new Set(doing.map((t) => t.goal_id)).size;
    return { k: "working", t: n > 1 ? `Working on ${n} goals` : "Working" };
  }
  if (!sessions.length) return { k: "asleep", t: "Not started" };
  if (m.sleeping || sessions.every((s) => s.status === "asleep")) return { k: "asleep", t: "Asleep" };
  return { k: "idle", t: "Idle" };
}

/** blockedOnPerson: the task can't move until a person decides something. */
export function blockedOnPerson(b: Board, t: TaskView): boolean {
  if (signalsForTask(b, t.id).some((s) => ["question", "gate", "dir_access", "review", "owner_change", "error"].includes(s.type))) return true;
  return (t.status === "doing" || t.status === "todo") && !!whyWaiting(b, runnerForTask(b, t));
}

/** blockReason explains a waiting task whose dependency is stuck on a person. */
export function blockReason(b: Board, t: TaskView): string {
  for (const id of t.depends_on) {
    const dep = task(b, id);
    if (!dep || dep.status === "done") continue;
    const s = signalsForTask(b, id)[0];
    if (s) {
      if (s.type === "gate" || s.type === "dir_access") return "Waiting on an approval";
      if (s.type === "review") return "Waiting on a review";
      if (s.type === "error") return "Waiting on a failed step";
      if (s.type === "owner_change") return `Waiting on ${firstName(b, payload(s).owner_user_id)}’s approval`;
      return "Waiting on an answer";
    }
    const why = whyWaiting(b, runnerForTask(b, dep));
    if (why) return why;
  }
  return "Waiting";
}

export const outputsOf = (b: Board, taskId: string) => b.outputs.filter((o) => o.task_id === taskId);
export { activeOutputs, outputGoal, unseen } from "./outputs";

export function fmtTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1) + "M";
  if (n >= 1000) return Math.round(n / 1000) + "k";
  return String(Math.round(n));
}

export function clock(iso: string | null | undefined): string {
  if (!iso) return "";
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function ago(iso: string | null | undefined): string {
  if (!iso) return "";
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 50) return "just now";
  if (s < 3600) return Math.round(s / 60) + " min ago";
  if (s < 86400) return Math.round(s / 3600) + " h ago";
  return Math.round(s / 86400) + " d ago";
}

export const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`;
