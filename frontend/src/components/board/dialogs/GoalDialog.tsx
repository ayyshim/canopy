"use client";

import Link from "next/link";
import { useState } from "react";
import { ApiError } from "@/lib/api";
import { giveGoal } from "@/lib/board/actions";
import { computer, computerFor, goal as findGoal, managers, signInLabel, staffOf, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { useB, useBoardCtx } from "../context";

/** GoalDialog gives a goal to a manager, choosing the branch it starts from and where it runs. */
export function GoalDialog({ managerId }: { managerId?: string }) {
  const b = useB();
  const { wsId, me } = useBoardCtx();
  const ui = useUI();
  const side = ui.side;
  const ctxManager = managerId ?? (side?.type === "goal" ? findGoal(b, side.id)?.manager_id : undefined) ?? managers(b).find((m) => visibleMember(b, m.id))?.id ?? "";
  const [manager, setManager] = useState(ctxManager ?? "");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [doneWhen, setDoneWhen] = useState("");
  const [base, setBase] = useState("");
  const [snapshot, setSnapshot] = useState(false);
  const runOptions = b.runners.filter((r) => r.owner_user_id === me || r.shared);
  const [runner, setRunner] = useState(computerFor(b, me) ?? "");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  // "Start from": the default branch, or the branch checked out in my clone on my computer.
  const repos = b.directories.filter((d) => d.is_repo);
  const myClone = b.clones.find((c) => c.user_id === me && repos.some((d) => d.id === c.directory_id) && c.runner_id === runner);
  const defaultBranch = repos[0]?.default_branch || "main";

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await giveGoal(wsId, { manager_id: manager, title, description: description.trim() || undefined, done_when: doneWhen, runner_id: runner || undefined, base, snapshot: snapshot && base === myClone?.current_branch });
      if (res.queued) ui.toast(res.reason ?? "You’re at your usage limit. The goal starts when it resets.", "warn");
      else ui.toast(`Goal given to ${visibleMember(b, manager)?.name}.`, "ok");
      ui.openDialog(null);
      if (res.goal) ui.openSide({ type: "goal", id: res.goal.id });
    } catch (err) {
      if (err instanceof ApiError) setErrors(Object.keys(err.fields).length ? err.fields : { title: err.message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="dialog" onSubmit={submit}>
      <h2>Give a goal</h2>
      <p>The manager analyzes it, splits it into tasks and works with staff to get it done.</p>
      <label className="field"><span>Manager</span>
        <select className="input" value={manager} onChange={(e) => setManager(e.target.value)}>
          {managers(b).map((mg) => {
            const m = visibleMember(b, mg.id);
            if (!m) return null;
            const n = staffOf(b, m.id).length;
            return <option key={m.id} value={m.id}>{m.name}{n ? ` and ${n} staff` : ""}</option>;
          })}
        </select>
        {errors.manager_id && <span className="err">{errors.manager_id}</span>}
      </label>
      <label className="field"><span>Goal</span>
        <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} required placeholder="Let admins refund a payment from the dashboard" autoComplete="off" autoFocus />
        {errors.title && <span className="err">{errors.title}</span>}
      </label>
      <label className="field"><span>Description <span style={{ fontWeight: 500 }}>(optional)</span></span>
        <textarea className="input" rows={4} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Add any details, context or links that help the team" />
      </label>
      <label className="field"><span>Done when <span style={{ fontWeight: 500 }}>(optional)</span></span>
        <input className="input" value={doneWhen} onChange={(e) => setDoneWhen(e.target.value)} placeholder="A refund shows up in the payment’s history" autoComplete="off" />
      </label>
      {repos.length > 0 && (
        <>
          <label className="field"><span>Start from</span>
            <select className="input" value={base} onChange={(e) => setBase(e.target.value)}>
              <option value="">{defaultBranch}, the default branch</option>
              {myClone && myClone.current_branch && myClone.current_branch !== defaultBranch && (
                <option value={myClone.current_branch}>{myClone.current_branch}, your current branch on {computer(b, myClone.runner_id)?.name}</option>
              )}
            </select>
            <span className="hint">Agents work on their own <code>canopy/…</code> branch in separate worktrees. Your working tree and checked-out branch are never touched.</span>
            {errors.base && <span className="err">{errors.base}</span>}
          </label>
          {myClone && myClone.dirty > 0 && base === myClone.current_branch && (
            <label className="toggle"><input type="checkbox" checked={snapshot} onChange={(e) => setSnapshot(e.target.checked)} /> Include my {myClone.dirty} uncommitted change{myClone.dirty > 1 ? "s" : ""} as a starting snapshot</label>
          )}
        </>
      )}
      {runOptions.length ? (
        <label className="field"><span>Runs on</span>
          <select className="input" value={runner} onChange={(e) => setRunner(e.target.value)}>
            {runOptions.map((r) => <option key={r.id} value={r.id}>{r.name}{r.online ? "" : " (offline)"}, {signInLabel(b, r.id)}</option>)}
          </select>
          <span className="hint">Your goals run on your own computer with your own sign-in, so they never use someone else’s account.</span>
          {errors.runner_id && <span className="err">{errors.runner_id}</span>}
        </label>
      ) : (
        <div className="note-box" style={{ background: "var(--amber-soft)" }}>
          You don’t have a computer connected and there’s no shared one, so this goal waits until you <Link className="linkbtn" href="/org/computers">connect a computer</Link>.
        </div>
      )}
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary" disabled={busy || !title.trim() || !manager}>Give goal</button>
      </div>
    </form>
  );
}
