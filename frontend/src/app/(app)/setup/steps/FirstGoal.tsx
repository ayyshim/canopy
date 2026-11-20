"use client";

import type { StepProps } from "../Setup";

/** First goal: what the workspace starts working on when it's created. */
export function FirstGoal({ p, set, computer, dirs }: StepProps) {
  const managers = p.members.filter((m) => m.kind === "manager");
  const g = p.first_goal ?? { manager_key: managers[0]?.key ?? "", title: "", description: "", done_when: "" };
  const update = (patch: Partial<typeof g>) => set((x) => ({ ...x, first_goal: { ...g, ...patch } }));
  return (
    <>
      <h1>What should your team do first?</h1>
      <p className="lead">Describe it in a sentence. Your manager will plan it and hand out the tasks. You can skip this and do it later.</p>
      <label className="field"><span>Who should lead it?</span>
        <select className="input" value={g.manager_key} onChange={(e) => update({ manager_key: e.target.value })}>
          {managers.map((m) => <option key={m.key} value={m.key}>{m.name || "Manager"}</option>)}
        </select></label>
      <label className="field"><span>The goal</span><input className="input" value={g.title} onChange={(e) => update({ title: e.target.value })} placeholder="Add CSV export to the payments dashboard" /></label>
      <label className="field"><span>Description <span style={{ fontWeight: 500 }}>(optional)</span></span><textarea className="input" rows={4} value={g.description ?? ""} onChange={(e) => update({ description: e.target.value })} placeholder="Add any details, context or links that help the team" /></label>
      <label className="field"><span>How will you know it’s done? <span style={{ fontWeight: 500 }}>(optional)</span></span><input className="input" value={g.done_when} onChange={(e) => update({ done_when: e.target.value })} placeholder="Admins can download last month’s payments as CSV" /></label>
      <div className="note-box">The work happens on {computer?.name ?? "your computer"}.{dirs.some((d) => d.is_repo) ? " Your original files are never changed directly. Changes are kept separate until you approve them." : ""}</div>
    </>
  );
}
