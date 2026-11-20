"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { managers, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { SKILL_DESC, SKILL_LABEL } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { defaultInstructions } from "../chat/ChatSettings";
import { Markdown } from "../panels/bits";

/** MemberDialog adds a manager, staff for a manager, or independent staff. */
export function MemberDialog({ mode, bossId }: { mode: "manager" | "staff" | "pool"; bossId?: string }) {
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const [kind, setKind] = useState(mode === "manager" ? "manager" : "staff");
  const [name, setName] = useState("");
  const [reports, setReports] = useState(mode === "staff" ? bossId ?? "" : "");
  const [skill, setSkill] = useState("developer");
  const [model, setModel] = useState(mode === "manager" ? "opus" : "sonnet");
  const [instr, setInstr] = useState<string | null>(null);
  const [preview, setPreview] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const text = instr ?? defaultInstructions(kind, skill);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api("POST", `/workspaces/${wsId}/members`, {
        name, kind, cli: "claude", model, instructions: text,
        ...(kind === "staff" ? { skill, reports_to: reports || null, set_reports_to: true } : {}),
      });
      ui.toast(`${name} joined the workspace.`, "ok");
      ui.openDialog(null);
    } catch (err) {
      if (err instanceof ApiError) setErrors(Object.keys(err.fields).length ? err.fields : { name: err.message });
    }
  };

  return (
    <form className="dialog" onSubmit={submit}>
      <h2>{kind === "manager" ? "Add a manager" : "Add staff"}</h2>
      <p>{kind === "manager" ? "Managers take goals from people, plan the work and lead their own staff." : "Staff do the tasks managers hand out. Independent staff help any manager."}</p>
      <div className="field"><span>Kind</span>
        <div className="seg" role="group" aria-label="Kind">
          <button type="button" aria-pressed={kind === "manager"} onClick={() => { setKind("manager"); setModel("opus"); }}>Manager</button>
          <button type="button" aria-pressed={kind !== "manager"} onClick={() => { setKind("staff"); setModel("sonnet"); }}>Staff</button>
        </div>
      </div>
      <label className="field"><span>Name</span>
        <input className="input" value={name} onChange={(e) => setName(e.target.value)} required placeholder="Morgan" autoComplete="off" autoFocus />
        {errors.name && <span className="err">{errors.name}</span>}
      </label>
      {kind === "staff" && (
        <div className="pgrid">
          <label className="field"><span>Works for</span>
            <select className="input" value={reports} onChange={(e) => setReports(e.target.value)}>
              <option value="">Independent</option>
              {managers(b).map((m) => <option key={m.id} value={m.id}>{visibleMember(b, m.id)?.name ?? "Manager"}</option>)}
            </select></label>
          <label className="field"><span>Skill</span>
            <select className="input" value={skill} onChange={(e) => setSkill(e.target.value)}>
              {Object.entries(SKILL_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select></label>
        </div>
      )}
      <div className="pgrid">
        <label className="field"><span>CLI</span><select className="input" disabled><option>Claude Code</option></select></label>
        <label className="field"><span>Model</span>
          <select className="input" value={model} onChange={(e) => setModel(e.target.value)}>
            <option value="opus">Opus</option><option value="sonnet">Sonnet</option><option value="haiku">Haiku</option>
          </select></label>
      </div>
      <p className="hint">{kind === "manager" ? "Takes goals from people, plans the work and leads its staff" : SKILL_DESC[skill]}. Runs on the computer of whoever the work is for, with that person’s sign-in.</p>
      <div className="field">
        <div className="row"><span className="spacer">Instructions (Markdown)</span>
          <div className="seg" role="group" aria-label="Instructions view">
            <button type="button" aria-pressed={!preview} onClick={() => setPreview(false)}>Write</button>
            <button type="button" aria-pressed={preview} onClick={() => setPreview(true)}>Preview</button>
          </div>
        </div>
        {preview ? <Markdown text={text} className="mdprev md" />
          : <textarea className="textarea mono" rows={7} style={{ fontSize: 12.5 }} value={text} onChange={(e) => setInstr(e.target.value)} />}
      </div>
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary" disabled={!name.trim()}>{kind === "manager" ? "Add manager" : "Add staff"}</button>
      </div>
    </form>
  );
}
