"use client";

import type { StepProps } from "../Setup";

/** First goal: what the workspace starts working on when it's created. */
export function FirstGoal({ p, set, computer, dirs }: StepProps) {
  const managers = p.members.filter((m) => m.kind === "manager");
  const g = p.first_goal ?? { manager_key: managers[0]?.key ?? "", title: "", done_when: "" };
  const update = (patch: Partial<typeof g>) => set((x) => ({ ...x, first_goal: { ...g, ...patch } }));
  const signIn = p.clis.find((c) => c.cli === "claude")?.mode === "api_key" ? "your API key" : "your Claude subscription";
  return (
    <>
      <h1>Give a manager its first goal</h1>
      <p className="lead">People give goals to managers. The manager analyzes the goal, splits it into tasks and works with staff to get it done.</p>
      <label className="field"><span>Manager</span>
        <select className="input" value={g.manager_key} onChange={(e) => update({ manager_key: e.target.value })}>
          {managers.map((m) => <option key={m.key} value={m.key}>{m.name || "Manager"}</option>)}
        </select></label>
      <label className="field"><span>Goal</span><input className="input" value={g.title} onChange={(e) => update({ title: e.target.value })} placeholder="Add CSV export to the payments dashboard" /></label>
      <label className="field"><span>Done when <span style={{ fontWeight: 500 }}>(optional)</span></span><input className="input" value={g.done_when} onChange={(e) => update({ done_when: e.target.value })} placeholder="Admins can download last month’s payments as CSV" /></label>
      <div className="note-box">It runs on {computer?.name ?? "your computer"} with {signIn}.{dirs.some((d) => d.is_repo) ? <> Agents work on a <code>canopy/…</code> branch in each repository, never in your working tree.</> : ""}</div>
    </>
  );
}
