"use client";

import { useState } from "react";
import { SkillPicker } from "@/components/skills/Skills";
import { api, ApiError } from "@/lib/api";
import { managers, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { SKILL_DESC, SKILL_LABEL, type MemberView } from "@/lib/types";
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
  const [skillIds, setSkillIds] = useState<string[]>([]);
  const [isolated, setIsolated] = useState(false);
  const [busy, setBusy] = useState(false);
  const [model, setModel] = useState(mode === "manager" ? "opus" : "sonnet");
  const [instr, setInstr] = useState<string | null>(null);
  const [preview, setPreview] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const text = instr ?? defaultInstructions(kind, skill);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const r = await api<{ member: MemberView }>("POST", `/workspaces/${wsId}/members`, {
        name, kind, cli: "claude", model, instructions: text,
        ...(kind === "staff" ? { skill, reports_to: reports || null, set_reports_to: true, isolated: !reports && isolated } : {}),
      });
      let missed = false;
      for (const id of skillIds) {
        try { await api("PUT", `/workspaces/${wsId}/members/${r.member.id}/skills/${id}`); } catch { missed = true; }
      }
      ui.toast(missed ? `${name} joined, but some skills didn’t save. Open ${name}’s settings to try again.` : `${name} joined your team.`, missed ? "info" : "ok");
      ui.openDialog(null);
    } catch (err) {
      setBusy(false);
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
      {!isMgr && !reports && (
        <label className="field"><span><input type="checkbox" checked={isolated} onChange={(e) => setIsolated(e.target.checked)} /> Isolated</span>
          <span className="hint">Only you can chat with them. Managers can’t borrow them.</span></label>
      )}
      <div className="field"><span>Skills <span style={{ fontWeight: 500 }}>(optional)</span></span>
        <SkillPicker skills={b.skills ?? []} selected={skillIds} onToggle={(id, on) => setSkillIds((x) => on ? [...x, id] : x.filter((y) => y !== id))}
          emptyHint={<>No skills yet. Add some in <a href={`/w/${wsId}/settings?tab=skills`}>workspace settings</a> and give them to {name || "anyone"} later.</>} />
        {(b.skills ?? []).length > 0 && <span className="hint">Skills are instructions {name || "they"} will follow, like your review checklist.</span>}
      </div>
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
        <button className="btn btn-primary" disabled={!name.trim() || busy}>{isMgr ? "Add manager" : "Add staff"}</button>
      </div>
    </form>
  );
}
