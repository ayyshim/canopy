"use client";

import Link from "next/link";
import { useState } from "react";
import { patchWorkspace } from "@/lib/board/actions";
import { computer, firstName, independentStaff, isAdmin, managers, signInLabel, staffOf, visibleMember, whyWaiting } from "@/lib/board/derive";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import type { GoalView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Avatar, RepoIcon } from "../icons";
import { GoalRow, goalNeeds } from "./bits";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";

const TABS: [string, string, (g: GoalView, needs: boolean) => boolean][] = [
  ["all", "All", () => true],
  ["active", "Active", (g) => g.status === "active" || g.status === "planning"],
  ["done", "Done", (g) => g.status === "done"],
  ["draft", "Drafts", (g) => g.status === "draft"],
  ["attn", "Need attention", (_g, needs) => needs],
];

export const FolderIcon = () => (
  <svg className="ficon" viewBox="0 0 20 20" aria-hidden="true"><path d="M2.5 5.5a1 1 0 0 1 1-1h4l2 2h7a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1h-13a1 1 0 0 1-1-1z" stroke="currentColor" strokeWidth="1.5" fill="none" /></svg>
);

/** WorkspacePanel opens from the workspace card: goals by tab, notes, directories, people. */
export function WorkspacePanel() {
  const b = useB();
  const { wsId } = useBoardCtx();
  const presence = useBoard((s) => s.presence);
  const ui = useUI();
  const admin = isAdmin(b);
  const [tab, setTab] = useState("all");
  const [name, setName] = useState(b.workspace.name);
  const [about, setAbout] = useState(b.workspace.about);
  const filter = TABS.find((t) => t[0] === tab)![2];
  const list = b.goals.filter((g) => filter(g, goalNeeds(b, g)));
  const done = b.tasks.filter((t) => t.status === "done").length;
  const hosts = [...new Set(b.goals.filter((g) => g.status !== "done" && g.status !== "draft").map((g) => g.runner_id).filter(Boolean))] as string[];
  const save = (patch: Record<string, string>) => patchWorkspace(wsId, patch);

  return (
    <>
      <PanelHead>
        {admin ? (
          <div className="stack-sm" style={{ gap: 6 }}>
            <label className="sr" htmlFor="p-wname">Workspace name</label>
            <input className="input" id="p-wname" value={name} onChange={(e) => setName(e.target.value)} onBlur={() => name.trim() && name !== b.workspace.name && save({ name })}
              style={{ fontWeight: 700, fontSize: 16, minHeight: 38 }} />
            <label className="sr" htmlFor="p-wabout">What it works on</label>
            <input className="input" id="p-wabout" value={about} onChange={(e) => setAbout(e.target.value)} onBlur={() => about !== b.workspace.about && save({ about })}
              placeholder="What this workspace works on" style={{ minHeight: 34, fontSize: 13 }} />
          </div>
        ) : (
          <><h2>{b.workspace.name}</h2><span className="hint">{b.workspace.about}</span></>
        )}
      </PanelHead>
      <PanelBody>
        <div className="sb-sec">
          <span className="lbl">Goals</span>
          <div className="gtabs" role="group" aria-label="Filter goals">
            {TABS.map(([k, label, f]) => (
              <button key={k} aria-pressed={tab === k} onClick={() => setTab(k)}>{label} {b.goals.filter((g) => f(g, goalNeeds(b, g))).length}</button>
            ))}
          </div>
          {list.length ? <div className="stack-sm" style={{ gap: 5 }}>{list.map((g) => <GoalRow key={g.id} g={g} />)}</div>
            : <p className="hint">{b.goals.length ? "No goals here." : "No goals yet. Give one to a manager."}</p>}
        </div>
        {admin && (
          <div className="sb-sec">
            <span className="lbl">When every task of a goal is done</span>
            <div className="seg" role="group" aria-label="Finishing goals">
              <button aria-pressed={b.workspace.goal_completion === "auto"} onClick={() => save({ goal_completion: "auto" })}>Mark done automatically</button>
              <button aria-pressed={b.workspace.goal_completion !== "auto"} onClick={() => save({ goal_completion: "manual" })}>Ask me first</button>
            </div>
            <span className="hint">Done goals’ tasks are hidden from the board. Open a goal to show them again. Goals that changed code always wait for their person to ship them, unless this is automatic: then the branch is kept.</span>
          </div>
        )}
        <div className="sb-sec">
          <span className="lbl">Notes and decisions</span>
          {b.notes.slice(0, 4).map((n) => (
            <button className="trow" key={n.id} onClick={() => ui.openSide({ type: "notes", id: n.id })}>
              <span className={`pill ${n.kind === "decision" ? "goal" : n.kind === "summary" ? "ok" : ""}`}>{n.kind[0].toUpperCase() + n.kind.slice(1)}</span>
              <span className="spacer">{n.title}</span>
            </button>
          ))}
          <button className="linkbtn" style={{ alignSelf: "flex-start" }} onClick={() => ui.openSide({ type: "notes" })}>{b.notes.length ? `All ${b.notes.length} notes` : "Write the first note"}</button>
          <span className="hint">Kept in the hub, so every member can read them while any computer is off.</span>
        </div>
        <div className="sb-sec">
          <span className="lbl">Directories</span>
          {b.directories.map((d) => {
            const n = new Set(b.grants.filter((g) => g.directory_id === d.id).map((g) => g.member_id)).size;
            const clones = b.clones.filter((c) => c.directory_id === d.id).length;
            return (
              <div className="trow" style={{ cursor: "default" }} key={d.id}>
                {d.is_repo ? <RepoIcon /> : <FolderIcon />}
                <span className="spacer" style={{ minWidth: 0 }}>
                  <code>{d.name}</code><br />
                  <span className="hint">{d.is_repo ? `Git repository, shared through git · ${clones} clone${clones === 1 ? "" : "s"}` : `Folder on ${d.owner_name}’s computer, synced to the hub`} · {n} member{n === 1 ? "" : "s"}</span>
                </span>
              </div>
            );
          })}
          {!b.directories.length && <p className="hint">None yet.</p>}
          {admin && <div><Link className="btn btn-sm" href={`/w/${wsId}/settings?tab=directories`}>+ Add directories</Link></div>}
        </div>
        <div className="sb-sec">
          <span className="lbl">Managers</span>
          {managers(b).map((mg) => {
            const m = visibleMember(b, mg.id);
            if (!m) return null;
            return (
              <button className="trow" key={m.id} onClick={() => ui.openChat(m.id)}>
                <Avatar name={m.name} color={m.avatar_color} size={28} />
                <span className="spacer"><b>{m.name}</b><br /><span className="hint">{staffOf(b, m.id).length} staff, {b.goals.filter((g) => g.manager_id === m.id).length} goals</span></span>
              </button>
            );
          })}
          <button className="trow" onClick={() => ui.openSide({ type: "pool" })}>
            <span className="avatar" style={{ background: "var(--teal)", width: 28, height: 28, fontSize: 10 }}>IN</span>
            <span className="spacer"><b>Independent staff</b><br /><span className="hint">{independentStaff(b).length} members</span></span>
          </button>
        </div>
        <div className="box ok">
          <span className="hint">Tasks</span><b>{done} of {b.tasks.length} done</b>
          <div className="meter"><i style={{ width: `${b.tasks.length ? (done / b.tasks.length) * 100 : 0}%` }} /></div>
        </div>
        <div className="sb-sec">
          <span className="lbl">People</span>
          <span style={{ fontSize: 13.5 }}>{presence.length} on the board now. <button className="linkbtn" onClick={() => ui.openSide({ type: "people" })}>Manage</button></span>
        </div>
        <div className="sb-sec">
          <span className="lbl">Where work runs</span>
          {hosts.map((id) => {
            const why = whyWaiting(b, id);
            const who = [...new Set(b.goals.filter((g) => g.status !== "done" && g.status !== "draft" && g.runner_id === id).map((g) => firstName(b, g.created_by)))];
            return (
              <div className="trow" style={{ cursor: "default" }} key={id}>
                <span className={`dot ${why ? "need" : "done"}`} />
                <span className="spacer"><b>{computer(b, id)?.name ?? "A removed computer"}</b><br /><span className="hint">{who.join(" and ")}’s goals · {signInLabel(b, id)}</span></span>
                <span className="hint">{why || "Available"}</span>
              </div>
            );
          })}
          {!hosts.length && <p className="hint">Nothing running.</p>}
          <span className="hint">Each person’s goals run on their own computer with their own sign-in. Subscriptions don’t share how much is left, so Canopy shows only when a CLI reports its limit. Caps apply to API keys.</span>
        </div>
      </PanelBody>
      <PanelFoot>
        <div className="stack-sm" style={{ width: "100%" }}>
          <div className="row">
            <button className="btn btn-sm btn-primary spacer" onClick={() => ui.openDialog({ kind: "goal" })}>Give a goal</button>
            <button className="btn btn-sm spacer" onClick={() => ui.openSide({ type: "people" })}>People</button>
            <button className="btn btn-sm spacer" onClick={() => ui.openDialog({ kind: "summary" })}>Summary</button>
          </div>
          {admin && <div className="row"><Link className="btn btn-sm" href={`/w/${wsId}/settings`}>Workspace settings</Link></div>}
        </div>
      </PanelFoot>
    </>
  );
}
