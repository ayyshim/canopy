"use client";

import { baseRole, useCatalog, skillLabel, skillDesc } from "@/lib/catalog";
import { GoodAtPicker } from "@/components/GoodAtPicker";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { patchMember, putGrants, suggestGrants } from "@/lib/board/actions";
import { isAdmin, managers, memberStatus, staffOf, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { type MemberView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { SkillPicker } from "@/components/skills/Skills";
import { friendly } from "@/lib/skills";
import { Avatar } from "../icons";
import { Markdown } from "../panels/bits";

const MODELS = [["opus", "Opus"], ["sonnet", "Sonnet"], ["haiku", "Haiku"]];

export function defaultInstructions(kind: string, skill?: string | null): string {
  if (kind === "manager") {
    return "## Role\nYou are a manager. You take goals from people, plan the work and lead your staff.\n\n## How to plan\n1. Read the goal, its done-when and the workspace notes\n2. Split it into small tasks with clear steps\n3. Assign each task to the staff member with the right skill\n4. Borrow independent staff only when your staff lack the skill\n\n## When to ask a person\n- A decision changes scope, cost or a public behavior\n- Two reasonable options exist and the notes are silent";
  }
  const t: Record<string, string> = {
    developer: "## Role\nYou build features and fix bugs in this workspace.\n\n## How to work\n- Read the task and its steps before changing code\n- Keep changes small and covered by tests\n- Ask your manager before changing a public API\n\n## Done means\n- Tests pass\n- A short note on what changed",
    reviewer: "## Role\nYou test and review every change before it lands.\n\n## How to work\n- Run the test suite and the edge cases in the task\n- Leave specific, actionable comments\n- Approve only when the done-when is met",
    researcher: "## Role\nYou investigate questions and write up findings.\n\n## How to work\n- Cite the files and docs you read\n- Lead with the answer, then the evidence",
    designer: "## Role\nYou make screens and visual assets.\n\n## How to work\n- Follow the brand guide\n- Share a screenshot for review before handing off",
    writer: "## Role\nYou write and edit docs.\n\n## How to work\n- Plain words, short sentences\n- Match the existing docs structure",
  };
  const base = t[skill ?? ""] ?? t[baseRole(skill)];
  if (!base) return "## Role\nDescribe what this member does.";
  return skill?.includes(".") ? `${base}\n\n## Focus\n${skillLabel(skill)}: ${skillDesc(skill)}` : base;
}

/** ChatSettings edits a member: name, skill, manager, model, instructions and directory access. */
export function ChatSettings({ m }: { m: MemberView }) {
  useCatalog();
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const admin = isAdmin(b);
  const [editing, setEditing] = useState(false);
  const [instr, setInstr] = useState(m.instructions || defaultInstructions(m.kind, m.skill));
  const [name, setName] = useState(m.name);
  const isM = m.kind === "manager";
  const patch = (body: Record<string, unknown>, done?: string) => patchMember(wsId, m.id, body).then((r) => { if (r && done) ui.toast(done, "ok"); });
  const remove = async () => {
    try {
      await api("DELETE", `/workspaces/${wsId}/members/${m.id}`, { open_tasks: "hand_to", hand_to: m.reports_to ?? managers(b).find((x) => x.id !== m.id)?.id ?? null });
      ui.closeChat(m.id);
      ui.toast(`${m.name} was removed.`, "ok");
    } catch (e) {
      ui.toast(e instanceof ApiError ? e.message : "Couldn’t remove the member.", "error");
    }
  };

  return (
    <div className="stack" style={{ fontSize: 13, paddingBottom: 6 }}>
      <div className="pgrid">
        <label className="field full"><span>Name</span>
          <input className="input" value={name} disabled={!admin} onChange={(e) => setName(e.target.value)} onBlur={() => name.trim() && name !== m.name && patch({ name: name.trim() }, "Renamed to " + name.trim())} /></label>
        {!isM && (
          <>
            <div className="field"><span>Good at</span>
              <GoodAtPicker value={m.skill ?? ""} disabled={!admin} onChange={(v) => patch({ skill: v })} /></div>
            <label className="field"><span>Works for</span>
              <select className="input" value={m.reports_to ?? ""} disabled={!admin}
                onChange={(e) => patch({ reports_to: e.target.value || null, set_reports_to: true, ...(e.target.value ? { isolated: false } : {}) }, e.target.value ? `${m.name} now reports to ${visibleMember(b, e.target.value)?.name}` : `${m.name} is now independent staff`)}>
                <option value="">Independent</option>
                {managers(b).map((x) => <option key={x.id} value={x.id}>{visibleMember(b, x.id)?.name ?? "Manager"}</option>)}
              </select></label>
          </>
        )}
        {!isM && !m.reports_to && (
          <label className="field full"><span><input type="checkbox" checked={m.isolated} disabled={!admin} onChange={(e) => patch({ isolated: e.target.checked })} /> Isolated</span>
            <span className="hint">Only you can chat with them. Managers can’t borrow them.</span></label>
        )}
        <label className="field"><span>CLI</span><select className="input" disabled><option>Claude Code</option></select></label>
        <label className="field"><span>Model</span>
          <select className="input" value={m.model} disabled={!admin} onChange={(e) => patch({ model: e.target.value })}>
            {MODELS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
          </select></label>
        <p className="hint full">{isM ? "Takes goals from people, plans the work and leads its staff" : skillDesc(m.skill ?? "")}. Runs with the sign-in of whoever the work is for.</p>
      </div>
      <div className="field">
        <div className="row">
          <span className="lbl spacer">Instructions (Markdown)</span>
          <div className="seg" role="group" aria-label="Instructions view">
            <button aria-pressed={!editing} onClick={() => setEditing(false)}>Preview</button>
            <button aria-pressed={editing} disabled={!admin} onClick={() => setEditing(true)}>Edit</button>
          </div>
        </div>
        {editing ? (
          <>
            <textarea className="textarea mono" rows={9} style={{ fontSize: 12.5 }} value={instr} onChange={(e) => setInstr(e.target.value)} aria-label={`Instructions for ${m.name}`} />
            <div className="row" style={{ justifyContent: "flex-end" }}>
              <button className="btn btn-sm" onClick={() => { setInstr(m.instructions || defaultInstructions(m.kind, m.skill)); setEditing(false); }}>Cancel</button>
              <button className="btn btn-sm btn-primary" onClick={() => patch({ instructions: instr }, `${m.name}’s instructions saved`).then(() => setEditing(false))}>Save instructions</button>
            </div>
          </>
        ) : <Markdown text={instr} className="mdprev md" />}
      </div>
      <div className="field">
        <span className="lbl">Skills</span>
        <SkillPicker skills={b.skills ?? []} selected={b.member_skills?.[m.id] ?? []} disabled={!admin}
          emptyHint={<>No skills in the library yet. {admin ? <a href={`/w/${wsId}/settings?tab=skills`}>Add some in workspace settings.</a> : "An admin can add some."}</>}
          onToggle={async (id, on) => {
            try { await api(on ? "PUT" : "DELETE", `/workspaces/${wsId}/members/${m.id}/skills/${id}`); } catch (e) { ui.toast(friendly(e), "error"); }
          }} />
        <span className="hint">Skills are instructions {m.name} follows. Changes apply the next time {m.name} starts a task.</span>
      </div>
      <DirAccess m={m} />
      {isM && (
        <div className="sb-sec">
          <span className="lbl">Staff</span>
          {staffOf(b, m.id).map((s) => {
            const x = visibleMember(b, s.id);
            if (!x) return null;
            const st = memberStatus(b, x);
            return (
              <button className="trow" key={x.id} onClick={() => ui.openChat(x.id)}>
                <Avatar name={x.name} color={x.avatar_color} size={28} />
                <span className="spacer"><b>{x.name}</b><br /><span className="hint">{skillLabel(x.skill ?? "")}, {x.model}</span></span>
                <span className={`dot ${st.k}`} title={st.t} />
              </button>
            );
          })}
          {!staffOf(b, m.id).length && <p className="hint">No staff of its own. It borrows independent staff.</p>}
        </div>
      )}
      <div className="row wrap">
        <button className="btn btn-sm" onClick={() => patch({ sleeping: !m.sleeping }, m.sleeping ? `${m.name} is awake` : `${m.name} is asleep`)}>{m.sleeping ? "Wake" : "Sleep"}</button>
        <button className="btn btn-sm" onClick={() => ui.openDialog({ kind: "task", memberId: m.id })}>Give a task</button>
        {admin && <button className="btn btn-sm btn-danger" onClick={remove}>Remove</button>}
      </div>
    </div>
  );
}

/** DirAccess: which workspace directories a member can reach. Staff get suggestions from their skill. */
export function DirAccess({ m }: { m: MemberView }) {
  const b = useB();
  const { wsId } = useBoardCtx();
  const admin = isAdmin(b);
  if (!b.directories.length) return null;
  const mine = new Map(b.grants.filter((g) => g.member_id === m.id).map((g) => [g.directory_id, g]));
  const boss = m.reports_to ? b.grants.filter((g) => g.member_id === m.reports_to).map((g) => g.directory_id) : null;
  const pool = b.directories.filter((d) => !boss || boss.includes(d.id));
  const suggested = [...mine.values()].every((g) => g.source === "suggested");
  const byName = m.reports_to ? visibleMember(b, m.reports_to)?.name ?? "Their manager" : "Canopy";
  const writes = m.kind === "manager" || ["developer", "designer", "writer"].includes(m.skill ?? "");
  const toggle = (dirId: string, on: boolean) => {
    const next = [...mine.values()].filter((g) => g.directory_id !== dirId).map((g) => ({ directory_id: g.directory_id, mode: g.mode }));
    if (on) next.push({ directory_id: dirId, mode: writes ? "read_write" : "read" });
    putGrants(wsId, m.id, next);
  };
  const resuggest = async () => {
    const r = await suggestGrants(wsId, m.id);
    if (r) putGrants(wsId, m.id, r.grants.map((g) => ({ directory_id: g.directory_id, mode: g.mode })));
  };

  if (m.kind === "manager") {
    return (
      <div className="field">
        <span className="lbl">Directory access</span>
        <div className="seg" role="group" aria-label="Directory access">
          <button aria-pressed={m.all_directories} disabled={!admin} onClick={() => putGrants(wsId, m.id, [], true)}>All directories</button>
          <button aria-pressed={!m.all_directories} disabled={!admin}
            onClick={() => putGrants(wsId, m.id, [...mine.values()].map((g) => ({ directory_id: g.directory_id, mode: g.mode })), false)}>Only selected</button>
        </div>
        {!m.all_directories && (
          <div className="chips">
            {b.directories.map((d) => (
              <label className="dchip" key={d.id}><input type="checkbox" checked={mine.has(d.id)} disabled={!admin} onChange={(e) => toggle(d.id, e.target.checked)} />{d.name}</label>
            ))}
          </div>
        )}
      </div>
    );
  }
  return (
    <div className="field">
      <span className="lbl">Directory access</span>
      <div className="chips">
        {pool.map((d) => (
          <label className="dchip" key={d.id}>
            <input type="checkbox" checked={mine.has(d.id)} disabled={!admin} onChange={(e) => toggle(d.id, e.target.checked)} />{d.name}
            {mine.has(d.id) && <small>{mine.get(d.id)!.mode === "read_write" ? "read & write" : "read"}</small>}
          </label>
        ))}
        {!pool.length && <span className="hint">Its manager has no directories to share.</span>}
      </div>
      <div className="row wrap">
        {suggested && mine.size > 0 ? <span className="pill info">Suggested by {byName}</span> : (
          <>
            {mine.size > 0 && <span className="pill">Changed by you</span>}
            {admin && <button type="button" className="linkbtn" onClick={resuggest}>Ask {byName} to suggest again</button>}
          </>
        )}
      </div>
    </div>
  );
}
