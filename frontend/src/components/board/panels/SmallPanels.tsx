"use client";

import { useCatalog, skillLabel } from "@/lib/catalog";
import Link from "next/link";
import { ago, computer, firstName, independentStaff, isAdmin, memberStatus, personName, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { useB, useBoardCtx } from "../context";
import { Avatar, RepoIcon } from "../icons";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";
import { FolderIcon } from "./WorkspacePanel";

/** PoolPanel: independent staff, and which managers they're helping now. */
export function PoolPanel() {
  useCatalog();
  const b = useB();
  const ui = useUI();
  const pool = independentStaff(b);
  return (
    <>
      <PanelHead>
        <div className="row">
          <span className="avatar" style={{ background: "var(--teal)" }}>IN</span>
          <div><h2>Independent staff</h2><span className="hint">Shared by every manager</span></div>
        </div>
      </PanelHead>
      <PanelBody>
        <p style={{ fontSize: 13.5 }}>Independent staff don’t report to a manager. Any manager can ask them for help when its own staff can’t cover a task.</p>
        <div className="sb-sec">
          <span className="lbl">Members</span>
          {pool.map((raw) => {
            const m = visibleMember(b, raw.id);
            if (!m) return null;
            const helping = [...new Set(b.tasks.filter((t) => t.member_id === m.id && t.status !== "done").map((t) => b.goals.find((g) => g.id === t.goal_id)?.manager_id).filter(Boolean))] as string[];
            const st = memberStatus(b, m);
            return (
              <button className="trow" key={m.id} onClick={() => ui.openChat(m.id)}>
                <Avatar name={m.name} color={m.avatar_color} size={28} />
                <span className="spacer"><b>{m.name}</b><br /><span className="hint">{skillLabel(m.skill ?? "")}, {m.model}</span>
                  {helping.length > 0 && <><br /><span style={{ fontSize: 12, color: "var(--teal-text)" }}>Helping {helping.map((id) => visibleMember(b, id)?.name).join(" and ")}</span></>}
                </span>
                <span className={`dot ${st.k}`} title={st.t} />
              </button>
            );
          })}
          {!pool.length && <p className="hint">No independent staff yet.</p>}
        </div>
      </PanelBody>
      {isAdmin(b) && <PanelFoot><button className="btn btn-sm" onClick={() => ui.openDialog({ kind: "member", mode: "pool" })}>+ Add independent staff</button></PanelFoot>}
    </>
  );
}

/** DirsPanel: each repository with its clones and goal branches, each folder with its sync state. */
export function DirsPanel() {
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  return (
    <>
      <PanelHead><span className="tag">Directories</span><h2>Repositories and folders</h2></PanelHead>
      <PanelBody>
        {b.directories.map((d) => {
          if (d.is_repo) {
            const clones = b.clones.filter((c) => c.directory_id === d.id);
            const branches = b.goals.filter((g) => g.status !== "done" && b.goal_repos.some((r) => r.goal_id === g.id && r.directory_id === d.id));
            return (
              <div className="box" key={d.id}>
                <div className="row"><RepoIcon /><b className="spacer">{d.name}</b><span className="pill">Git repository</span></div>
                <code style={{ fontSize: 12 }}>{d.remote} · default {d.default_branch || "main"}</code>
                <span style={{ fontSize: 12.5 }}>Shared through git, not file sync. The hub keeps no copy of the code, only who has a clone and each goal’s branch.</span>
                <span className="lbl" style={{ marginTop: 4 }}>Clones</span>
                {clones.map((c) => (
                  <span key={c.runner_id} style={{ fontSize: 12.5 }}>
                    {personName(b, c.user_id)} on {computer(b, c.runner_id)?.name ?? "a computer"}: <code>{c.current_branch || "?"}</code>{c.dirty ? `, ${c.dirty} uncommitted` : ", clean"}
                  </span>
                ))}
                {!clones.length && <span className="hint">Nobody has linked a clone yet. Computers without one clone it when a goal needs it.</span>}
                {branches.length > 0 && (
                  <>
                    <span className="lbl" style={{ marginTop: 4 }}>Goal branches</span>
                    {branches.map((g) => (
                      <button className="trow" key={g.id} onClick={() => ui.openSide({ type: "goal", id: g.id })}>
                        <span className="gdot" style={{ background: g.color }} /><code className="spacer">{g.branch}</code>
                        <span className="hint">from {g.base || d.default_branch || "main"} · {firstName(b, g.created_by)}</span>
                      </button>
                    ))}
                  </>
                )}
              </div>
            );
          }
          const r = computer(b, d.machine_id);
          const mir = b.mirrors[d.id];
          const q = mir?.queued_writes ?? 0;
          return (
            <div className="box" key={d.id}>
              <div className="row"><FolderIcon /><b className="spacer">{d.name}</b>{r?.online ? <span className="pill ok">owner online</span> : <span className="pill warn">owner offline</span>}</div>
              <code style={{ fontSize: 12 }}>{d.path}</code>
              <span style={{ fontSize: 12.5 }}>
                A folder, not a repository. Source of truth: {d.owner_name}’s {r?.name ?? "computer"}. The hub copy is {r?.online ? "in sync" : mir?.last_synced_at ? "from " + ago(mir.last_synced_at) : "not made yet"}; agents keep reading it while the owner is offline.
              </span>
              {q > 0 && <span className="pill info">{q} change{q > 1 ? "s" : ""} waiting to land on {d.owner_name}’s disk</span>}
              <span className="hint">Changes by people without write access need {d.owner_name.split(" ")[0]}’s approval and run on {d.owner_name.split(" ")[0]}’s computer.</span>
            </div>
          );
        })}
        {!b.directories.length && <p className="hint">No directories yet.</p>}
        <span className="hint">Repositories skip nothing and upload nothing. Folders skip .gitignore, .dockerignore and .canopyignore entries, build output and secret files like .env and keys.</span>
      </PanelBody>
      <PanelFoot>
        <span className="spacer" />
        <Link className="btn btn-sm" href={`/w/${wsId}/settings?tab=directories`}>Manage directories</Link>
      </PanelFoot>
    </>
  );
}
