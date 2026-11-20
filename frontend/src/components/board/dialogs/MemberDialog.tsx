"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { managers, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { SKILL_DESC, SKILL_LABEL } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { defaultInstructions } from "../chat/ChatSettings";
import { Markdown } from "../panels/bits";

/** MemberDialog adds a manager or a staff member in plain words; model and instructions are tucked under Advanced. */
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
      ui.toast(`${name} joined your team.`, "ok");
      ui.openDialog(null);
    } catch (err) {
      if (err instanceof ApiError) setErrors(Object.keys(err.fields).length ? err.fields : { name: err.message });
    }
  };

  const isMgr = kind === "manager";
  const pick = (k: "manager" | "staff") => { setKind(k); setModel(k === "manager" ? "opus" : "sonnet"); };
  const choice = (k: "manager" | "staff", title: string, sub: string) => (
    <button type="button" className="choice" aria-pressed={kind === k} onClick={() => pick(k)}>
      <span style={{ textAlign: "left" }}><b>{title}</b><br /><span className="hint">{sub}</span></span>
    </button>
  );

  return (
    <form className="dialog" onSubmit={submit}>
      <h2>Add to your team</h2>
      <div className="field"><span>Who are you adding?</span>
        {choice("manager", "Manager", "Takes your goals, plans the work and leads staff.")}
        {choice("staff", "Staff", "Does the tasks a manager hands out.")}
      </div>
      <label className="field"><span>Name</span>
        <input className="input" value={name} onChange={(e) => setName(e.target.value)} required placeholder="Morgan" autoComplete="off" autoFocus />
        {errors.name && <span className="err">{errors.name}</span>}
      </label>
      {!isMgr && (
        <div className="pgrid">
          <label className="field"><span>Good at</span>
            <select className="input" value={skill} onChange={(e) => setSkill(e.target.value)}>
              {Object.entries(SKILL_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select></label>
          <label className="field"><span>Reports to</span>
            <select className="input" value={reports} onChange={(e) => setReports(e.target.value)}>
              <option value="">Anyone (shared staff)</option>
              {managers(b).map((m) => <option key={m.id} value={m.id}>{visibleMember(b, m.id)?.name ?? "Manager"}</option>)}
            </select></label>
        </div>
      )}
      {!isMgr && <p className="hint">{SKILL_DESC[skill]}.</p>}
      <details className="instr">
        <summary>Advanced</summary>
        <div className="stack-sm" style={{ marginTop: 8 }}>
          <label className="field"><span>How capable</span>
            <select className="input" value={model} onChange={(e) => setModel(e.target.value)}>
              <option value="opus">Most capable (slower)</option><option value="sonnet">Balanced</option><option value="haiku">Fastest (simpler work)</option>
            </select></label>
          <div className="field">
            <div className="row"><span className="spacer">Custom instructions</span>
              <div className="seg" role="group" aria-label="Instructions view">
                <button type="button" aria-pressed={!preview} onClick={() => setPreview(false)}>Write</button>
                <button type="button" aria-pressed={preview} onClick={() => setPreview(true)}>Preview</button>
              </div>
            </div>
            {preview ? <Markdown text={text} className="mdprev md" />
              : <textarea className="textarea" rows={7} style={{ fontSize: 13 }} value={text} onChange={(e) => setInstr(e.target.value)} />}
          </div>
        </div>
      </details>
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary" disabled={!name.trim()}>{isMgr ? "Add manager" : "Add staff"}</button>
      </div>
    </form>
  );
}
