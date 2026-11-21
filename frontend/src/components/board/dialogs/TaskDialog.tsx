"use client";

import { useCatalog, skillLabel } from "@/lib/catalog";
import { useState } from "react";
import { ApiError } from "@/lib/api";
import { attachFiles, createTask } from "@/lib/board/actions";
import { goal as findGoal, independentStaff, managers, staffOf, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { type MemberView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { FilePicker, MAX_FILE_MB } from "../panels/Attachments";

/** TaskDialog adds a task by hand, under a goal or on its own, waiting on other tasks. */
export function TaskDialog({ goalId, memberId }: { goalId?: string; memberId?: string }) {
  useCatalog();
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const g = findGoal(b, goalId);
  const [title, setTitle] = useState("");
  const [goal, setGoal] = useState(goalId ?? "");
  const [member, setMember] = useState(memberId ?? g?.manager_id ?? "");
  const [deps, setDeps] = useState<string[]>([]);
  const [files, setFiles] = useState<File[]>([]);
  const [error, setError] = useState("");
  const vis = (id: string) => visibleMember(b, id);
  const team = (mgr: MemberView) => [mgr, ...staffOf(b, mgr.id).map((s) => vis(s.id)).filter(Boolean)] as MemberView[];
  const asksManager = !!goal && vis(member)?.kind === "manager";
  const open = b.tasks.filter((t) => t.kind !== "plan" && t.status !== "done" && (!goal || t.goal_id === goal));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const { task } = await createTask(wsId, goal || undefined, { title, member_id: member, depends_on: deps });
      if (files.length) await attachFiles({ taskId: task.id }, files);
      ui.toast("Task created.", "ok");
      ui.openDialog(null);
    } catch (err) {
      setError(err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : "Couldn’t create the task.");
    }
  };

  return (
    <form className="dialog" onSubmit={submit}>
      <h2>{g?.status === "ready" || g?.status === "done" ? "Add a follow-up task" : "New task"}</h2>
      <p>{g?.status === "ready" || g?.status === "done" ? `This puts “${g.title}” back to work.` : "Most work comes from managers splitting goals. Add a task yourself when you want something specific done."}</p>
      <label className="field"><span>Title</span>
        <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} required placeholder="Add a saved-cards picker" autoComplete="off" autoFocus /></label>
      <label className="field"><span>Goal</span>
        <select className="input" value={goal} onChange={(e) => setGoal(e.target.value)}>
          <option value="">No goal</option>
          {b.goals.filter((x) => x.status !== "done" || x.id === goalId).map((x) => <option key={x.id} value={x.id}>{x.title}</option>)}
        </select></label>
      <label className="field"><span>Assign to</span>
        <select className="input" value={member} onChange={(e) => setMember(e.target.value)} required>
          <option value="" disabled>Choose a member</option>
          {managers(b).map((mg) => {
            const m = vis(mg.id);
            return m ? (
              <optgroup key={m.id} label={`${m.name}’s team`}>
                {team(m).map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "staff" ? ", " + skillLabel(x.skill ?? "") : ""}</option>)}
              </optgroup>
            ) : null;
          })}
          {independentStaff(b).some((s) => vis(s.id)) && (
            <optgroup label="Independent staff">
              {independentStaff(b).map((s) => vis(s.id)).filter(Boolean).map((x) => <option key={x!.id} value={x!.id}>{x!.name}, {skillLabel(x!.skill ?? "")}</option>)}
            </optgroup>
          )}
        </select>
        {asksManager && <span className="hint">Request → manager splits it up. {vis(member)?.name} won’t do it, and will hand it to the right staff.</span>}
      </label>
      <div className="field"><span>Waits on</span>
        <div className="chks">
          {open.map((t) => (
            <label key={t.id}><input type="checkbox" checked={deps.includes(t.id)} onChange={(e) => setDeps(e.target.checked ? [...deps, t.id] : deps.filter((d) => d !== t.id))} />{t.title}</label>
          ))}
          {!open.length && <span className="hint">No open tasks</span>}
        </div>
      </div>
      <FilePicker files={files} onChange={setFiles} />
      {error && <span className="err">{error}</span>}
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary" disabled={!title.trim() || !member || files.some((f) => f.size > MAX_FILE_MB << 20)}>Create task</button>
      </div>
    </form>
  );
}
