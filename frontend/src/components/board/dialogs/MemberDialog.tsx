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

const KINDS = [
  { id: "manager", title: "Manager", line: "Plans and leads. Takes goals from people and hands out tasks." },
  { id: "staff", title: "Staff", line: "Does the work. Builds, reviews, researches or writes." },
] as const;

/** MemberDialog adds a manager, staff for a manager, or independent staff. Type a name and press Add: the rest has good defaults. */
export function MemberDialog({ mode, bossId }: { mode: "manager" | "staff" | "pool"; bossId?: string }) {
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const [kind, setKind] = useState<"manager" | "staff">(mode === "manager" ? "manager" : "staff");
  const [name, setName] = useState("");
  const [reports, setReports] = useState(mode === "staff" ? bossId ?? "" : "");
  const [skill, setSkill] = useState("developer");
  const [skillIds, setSkillIds] = useState<string[]>([]);
  const [model, setModel] = useState<string | null>(null);
  const [instr, setInstr] = useState<string | null>(null);
  const [preview, setPreview] = useState(false);
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const text = instr ?? defaultInstructions(kind, skill);
  const theModel = model ?? (kind === "manager" ? "opus" : "sonnet");
  const library = b.skills ?? [];

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErrors({});
    try {
      const r = await api<{ member: MemberView }>("POST", `/workspaces/${wsId}/members`, {
        name, kind, cli: "claude", model: theModel, instructions: text,
        ...(kind === "staff" ? { skill, reports_to: reports || null, set_reports_to: true } : {}),
      });
      let missed = false;
      for (const id of skillIds) {
        try { await api("PUT", `/workspaces/${wsId}/members/${r.member.id}/skills/${id}`); } catch { missed = true; }
      }
      ui.toast(missed ? `${name} joined, but some skills couldn’t be added. Open ${name}’s settings to retry.` : `${name} joined the workspace.`, missed ? "info" : "ok");
      ui.openDialog(null);
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError) setErrors(Object.keys(err.fields).length ? err.fields : { name: err.message });
    }
  };

  return (
    <form className="dialog" onSubmit={submit}>
      <h2>Add a teammate</h2>
      <div className="field"><span>What will they do?</span>
        <div className="kindcards" role="group" aria-label="Kind of teammate">
          {KINDS.map((k) => (
            <button type="button" key={k.id} className="kindcard" aria-pressed={kind === k.id} onClick={() => setKind(k.id)}>
              <b>{k.title}</b><span>{k.line}</span>
            </button>
          ))}
        </div>
      </div>
      <label className="field"><span>Name</span>
        <input className="input" value={name} onChange={(e) => setName(e.target.value)} required placeholder={kind === "manager" ? "Atlas" : "Morgan"} autoComplete="off" autoFocus />
        {errors.name && <span className="err">{errors.name}</span>}
      </label>
      {kind === "staff" && (
        <div className="pgrid">
          <label className="field"><span>Role</span>
            <select className="input" value={skill} onChange={(e) => setSkill(e.target.value)}>
              {Object.entries(SKILL_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
            <span className="hint">{SKILL_DESC[skill]}</span></label>
          <label className="field"><span>Works for</span>
            <select className="input" value={reports} onChange={(e) => setReports(e.target.value)}>
              <option value="">Any manager</option>
              {managers(b).map((m) => <option key={m.id} value={m.id}>{visibleMember(b, m.id)?.name ?? "Manager"}</option>)}
            </select></label>
        </div>
      )}
      <div className="field"><span>Skills <span style={{ fontWeight: 500 }}>(optional)</span></span>
        <SkillPicker skills={library} selected={skillIds} onToggle={(id, on) => setSkillIds((x) => on ? [...x, id] : x.filter((y) => y !== id))}
          emptyHint={<>The workspace has no skills yet. Add some in <a href={`/w/${wsId}/settings?tab=skills`}>workspace settings</a> and give them to {name || "anyone"} later.</>} />
        {library.length > 0 && <span className="hint">Skills are instructions {name || "they"} will follow, like your review checklist.</span>}
      </div>
      <details className="instr">
        <summary>More options</summary>
        <div className="stack-sm" style={{ marginTop: 8 }}>
          <label className="field"><span>Model</span>
            <select className="input" value={theModel} onChange={(e) => setModel(e.target.value)}>
              <option value="opus">Opus (most capable)</option><option value="sonnet">Sonnet (balanced)</option><option value="haiku">Haiku (fastest)</option>
            </select></label>
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
          <p className="hint">Runs on the computer of whoever the work is for, with that person’s sign-in.</p>
        </div>
      </details>
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary" disabled={!name.trim() || busy}>{kind === "manager" ? "Add manager" : "Add staff member"}</button>
      </div>
    </form>
  );
}
