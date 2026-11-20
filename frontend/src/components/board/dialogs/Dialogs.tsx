"use client";

import { useEffect, useState } from "react";
import { resolveSignal } from "@/lib/board/actions";
import { account, computer, fmtTokens, payload, personName, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { SKILL_LABEL, isHidden, type MemberView } from "@/lib/types";
import { useB } from "../context";
import { goalLabel } from "../panels/bits";
import { AccessDialog } from "./AccessDialog";
import { DraftDialog } from "./DraftDialog";
import { GoalDialog } from "./GoalDialog";
import { MemberDialog } from "./MemberDialog";
import { TaskDialog } from "./TaskDialog";
import { OutputThumb } from "../OutputThumb";

/** Dialogs shows the open dialog over the board. Esc or a click outside closes it. */
export function Dialogs() {
  const d = useUI((s) => s.dialog);
  const close = useUI((s) => s.openDialog);
  useEffect(() => {
    if (!d) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close(null);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [d, close]);
  if (!d) return null;
  let body: React.ReactNode;
  switch (d.kind) {
    case "goal": body = <GoalDialog key={d.draftId} managerId={d.managerId} draftId={d.draftId} />; break;
    case "task": body = <TaskDialog goalId={d.goalId} memberId={d.memberId} />; break;
    case "member": body = <MemberDialog mode={d.mode} bossId={d.bossId} />; break;
    case "invite": body = <AccessDialog />; break;
    case "access": body = <AccessDialog userId={d.userId} />; break;
    case "summary": body = <SummaryDialog />; break;
    case "cap": body = <CapDialog signalId={d.signalId} />; break;
    case "draft": body = <DraftDialog id={d.id} />; break;
    case "shortcuts": body = <ShortcutsDialog />; break;
  }
  return <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && close(null)}>{body}</div>;
}

function SummaryDialog() {
  const b = useB();
  const ui = useUI();
  const done = b.tasks.filter((t) => t.status === "done").length;
  const members = b.members.filter((m) => !isHidden(m)) as MemberView[];
  const per = members.map((m) => ({ m, tok: b.sessions.filter((z) => z.member_id === m.id).reduce((a, z) => a + z.tokens_in + z.tokens_out, 0) }));
  const total = per.reduce((a, x) => a + x.tok, 0) || 1;
  const borrowed = [...new Set(b.tasks.filter((t) => t.borrowed).map((t) => `${b.goals.find((g) => g.id === t.goal_id)?.manager_id}>${t.member_id}`))]
    .map((k) => k.split(">")).filter(([a, c]) => visibleMember(b, a) && visibleMember(b, c));
  const decisions = b.notes.filter((n) => n.kind === "decision").slice(0, 8);
  return (
    <div className="dialog" style={{ width: "min(640px,100%)" }}>
      <h2>{b.workspace.name}, workspace summary</h2>
      <p>Managers: {members.filter((m) => m.kind === "manager").map((m) => m.name).join(" and ") || "none yet"}.</p>
      <span className="lbl">Goals</span>
      <div className="stack-sm" style={{ gap: 5 }}>
        {b.goals.map((g) => (
          <button className="trow" key={g.id} onClick={() => { ui.openDialog(null); ui.openSide({ type: "goal", id: g.id }); }}>
            <span className="gdot" style={{ background: g.color }} />
            <span className="spacer">{g.title}<br /><span className="hint">Given by {personName(b, g.created_by)} to {visibleMember(b, g.manager_id)?.name ?? "a manager"} · runs on {computer(b, g.runner_id)?.name ?? "no computer yet"}</span></span>
            <b>{goalLabel(b, g)}</b>
          </button>
        ))}
        {!b.goals.length && <p className="hint">No goals yet</p>}
      </div>
      <div className="stats">
        <div><span>Tasks done</span><b>{done}/{b.tasks.length}</b></div>
        <div><span>Outputs</span><b>{b.outputs.length}</b></div>
        <div><span>Notes</span><b>{b.notes.length}</b></div>
      </div>
      {borrowed.length > 0 && (
        <><span className="lbl">Teamwork</span><ul className="list">{borrowed.map(([a, c]) => <li key={a + c}>{visibleMember(b, a)?.name} borrowed {visibleMember(b, c)?.name} from independent staff</li>)}</ul></>
      )}
      {b.outputs.length > 0 && (
        <><span className="lbl">Outputs</span>
          <div className="ocards">{b.outputs.map((o) => (
            <div className="ocard" style={{ width: 150 }} key={o.id} role="button" tabIndex={0} onClick={() => { ui.openDialog(null); ui.openViewer(o.id); }}>
              <div className="thumb" style={{ height: 84 }}><OutputThumb o={o} /></div><div className="meta"><b>{o.title}</b></div>
            </div>
          ))}</div></>
      )}
      <span className="lbl">Usage by member</span>
      <ul className="list">
        {per.map((x) => (
          <li key={x.m.id}>
            <span className="spacer">{x.m.name}, {x.m.kind === "manager" ? "Manager" : SKILL_LABEL[x.m.skill ?? ""]}<br /><span className="hint">{x.m.model}, billed to whoever gave the work</span></span>
            <b className="tnum">{fmtTokens(x.tok)} tokens</b><span className="hint tnum" style={{ width: 40, textAlign: "right" }}>{Math.round((x.tok / total) * 100)}%</span>
          </li>
        ))}
      </ul>
      <span className="lbl">Decisions</span>
      {decisions.length ? <ul className="list">{decisions.map((n) => <li key={n.id}><span className="spacer">{n.title}</span></li>)}</ul> : <p className="hint">No decisions written down yet.</p>}
      {b.sessions.some((z) => z.recovered_from) && <p className="hint">A session crashed and recovered on its own. No work was lost.</p>}
      <div className="row"><span className="spacer" /><button className="btn" onClick={() => ui.openDialog(null)}>Close</button></div>
    </div>
  );
}

function CapDialog({ signalId }: { signalId: string }) {
  const b = useB();
  const ui = useUI();
  const s = b.signals.find((x) => x.id === signalId);
  const p = s ? payload(s) : {};
  const a = account(b, p.runner_id);
  const [cap, setCap] = useState(String((a?.cap_tokens_per_day ?? p.cap ?? 1_000_000) * 2));
  if (!s) return null;
  return (
    <form className="dialog" onSubmit={async (e) => { e.preventDefault(); if (await resolveSignal(s.id, "raise_cap", { cap: Number(cap) })) ui.openDialog(null); }}>
      <h2>Raise today’s cap</h2>
      <p>{p.runner_name} resumes when the cap is above what it used today ({fmtTokens(a?.used_tokens ?? p.used ?? 0)} tokens).</p>
      <label className="field"><span>Daily cap (tokens)</span><input className="input tnum" value={cap} onChange={(e) => setCap(e.target.value.replace(/\D/g, ""))} inputMode="numeric" /></label>
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary">Raise cap</button>
      </div>
    </form>
  );
}

function ShortcutsDialog() {
  const ui = useUI();
  const rows = [["Drag", "Pan"], ["Wheel / pinch", "Zoom (25% to 220%)"], ["F", "Fit the tree"], ["] and [", "Walk items that need you"], ["G", "Give a goal"],
    ["N", "New task"], ["O", "Outputs"], ["Alt + ←", "Back in the details panel"], ["?", "This list"], ["Esc", "Close the top-most layer"]];
  return (
    <div className="dialog">
      <h2>Keyboard shortcuts</h2>
      <table className="table"><tbody>{rows.map(([k, v]) => <tr key={k}><td><span className="kbd">{k}</span></td><td>{v}</td></tr>)}</tbody></table>
      <div className="row"><span className="spacer" /><button className="btn" onClick={() => ui.openDialog(null)}>Close</button></div>
    </div>
  );
}
