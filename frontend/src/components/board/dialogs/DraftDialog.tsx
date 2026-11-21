"use client";

import { GoodAtPicker } from "@/components/GoodAtPicker";
import { useState } from "react";
import { approveDraft, pendingDrafts, rejectDraft } from "@/lib/board/proposals";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { isAdmin, visibleMember } from "@/lib/board/derive";
import { type StaffProposalView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Markdown } from "../panels/bits";

/** DraftDialog shows a manager's draft of a new staff member or of new instructions. Admins edit,
 * approve or reject it; everyone else reads it. */
export function DraftDialog({ id }: { id: string }) {
  const proposals = useBoard((s) => s.proposals);
  const ui = useUI();
  const p = proposals.find((x) => x.id === id);
  if (!p) return <div className="dialog"><h2>Draft not found</h2><div className="row"><span className="spacer" /><button className="btn" onClick={() => ui.openDialog(null)}>Close</button></div></div>;
  const pending = pendingDrafts(proposals);
  const at = pending.findIndex((x) => x.id === id);
  const step = (n: number) => ui.openDialog({ kind: "draft", id: pending[(at + n + pending.length) % pending.length].id });
  // key resets the edits when the admin moves to another draft
  return <DraftForm key={p.id} p={p} nav={at >= 0 && pending.length > 1 ? { at, total: pending.length, step } : null} />;
}

function DraftForm({ p, nav }: { p: StaffProposalView; nav: { at: number; total: number; step: (n: number) => void } | null }) {
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const admin = isAdmin(b);
  const isNew = p.kind === "new";
  const target = p.member_id ? visibleMember(b, p.member_id) : undefined;
  const [name, setName] = useState(p.name);
  const [role, setRole] = useState(p.role ?? "developer");
  const [instr, setInstr] = useState(p.instructions);
  const [skills, setSkills] = useState(p.skills);
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const open = p.status === "pending";
  const editable = admin && open;
  const manager = visibleMember(b, p.manager_id)?.name ?? "A manager";
  const oldInstr = target && "instructions" in target ? target.instructions : "";

  const finish = async (ok: Promise<boolean>) => {
    setBusy(true);
    if (await ok) {
      const next = pendingDrafts(useBoard.getState().proposals).find((x) => x.id !== p.id);
      ui.openDialog(next ? { kind: "draft", id: next.id } : null);
    } else setBusy(false);
  };

  return (
    <div className="dialog" style={{ width: "min(640px,100%)" }}>
      <h2>{isNew ? "New staff member" : `New instructions for ${target && "name" in target ? target.name : "a staff member"}`}</h2>
      <p>{manager} {open ? "wants" : "wanted"} {isNew ? "to add this person to the team." : "to change these instructions."}
        {!open && <> <b>{p.status === "approved" ? "Approved." : "Rejected."}</b>{p.reason && <> {p.reason}</>}</>}
        {!admin && open && <> An admin will decide.</>}
      </p>
      {p.note && <blockquote className="hint" style={{ margin: "0 0 12px", paddingLeft: 12, borderLeft: "3px solid var(--line)" }}><b>Why:</b> {p.note}</blockquote>}
      {isNew && (
        <div className="pgrid">
          <label className="field"><span>Name</span>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} readOnly={!editable} /></label>
          <div className="field"><span>Good at</span>
            <GoodAtPicker value={role} onChange={setRole} disabled={!editable} /></div>
        </div>
      )}
      {isNew && <p className="hint">Reports to {manager}.</p>}
      {!isNew && (
        <div className="field"><span>Current instructions</span>
          {oldInstr ? <Markdown text={oldInstr} className="mdprev md" /> : <p className="hint">None yet.</p>}
        </div>
      )}
      <label className="field"><span>{isNew ? "Instructions" : "New instructions"}</span>
        <textarea className="textarea" rows={8} style={{ fontSize: 13 }} value={instr} onChange={(e) => setInstr(e.target.value)} readOnly={!editable} />
      </label>
      <div className="field"><span>Skills</span>
        {skills.length === 0 && <p className="hint">No skills drafted.</p>}
        <div className="stack-sm" style={{ gap: 6 }}>
          {skills.map((s, i) => (
            <details className="instr" key={s.name + i}>
              <summary>{s.name}{s.source_url ? " · from the web" : " · written by the manager"}</summary>
              <div className="stack-sm" style={{ marginTop: 8 }}>
                {s.source_url && <a href={s.source_url} target="_blank" rel="noreferrer noopener">{s.source_url}</a>}
                <Markdown text={s.content} className="mdprev md" />
                {editable && <div className="row"><button type="button" className="btn btn-sm" onClick={() => setSkills(skills.filter((_, j) => j !== i))}>Remove skill</button></div>}
              </div>
            </details>
          ))}
        </div>
      </div>
      {rejecting && (
        <label className="field"><span>Why? <span style={{ fontWeight: 500 }}>(optional, the manager can read it)</span></span>
          <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} autoFocus /></label>
      )}
      <div className="row">
        {nav && <><button type="button" className="btn btn-sm" onClick={() => nav.step(-1)} aria-label="Previous draft">‹</button>
          <span className="hint">{nav.at + 1} of {nav.total}</span>
          <button type="button" className="btn btn-sm" onClick={() => nav.step(1)} aria-label="Next draft">›</button></>}
        <span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Close</button>
        {editable && !rejecting && <button type="button" className="btn" disabled={busy} onClick={() => setRejecting(true)}>Reject</button>}
        {editable && rejecting && <button type="button" className="btn" disabled={busy} onClick={() => finish(rejectDraft(wsId, p.id, reason))}>Reject draft</button>}
        {editable && !rejecting && (
          <button type="button" className="btn btn-primary" disabled={busy || (isNew && !name.trim()) || !instr.trim()}
            onClick={() => finish(approveDraft(wsId, p.id, { instructions: instr, skills, ...(isNew ? { name: name.trim(), role } : {}) }))}>Approve</button>
        )}
      </div>
    </div>
  );
}
