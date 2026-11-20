"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { isAdmin, managers, visibleMember } from "@/lib/board/derive";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { useB, useBoardCtx } from "../context";
import { Avatar } from "../icons";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";

export type Access = { user_id: string; name: string; email: string; avatar_color: string; role: "admin" | "staff"; manager_ids: string[]; independent_staff: boolean };
type Invite = { id: string; email: string; workspaces?: { workspace_id: string; role: string; manager_ids?: string[] }[] };

/** useAccess loads who can see this workspace (admins only); reload after changes. */
export function useAccess(wsId: string, admin: boolean) {
  const [list, setList] = useState<Access[] | null>(null);
  const reload = useCallback(() => {
    if (!admin) return;
    api<{ access: Access[] }>("GET", `/workspaces/${wsId}/access`).then((r) => setList(r.access), () => setList([]));
  }, [wsId, admin]);
  useEffect(reload, [reload]);
  return { list, reload };
}

export function PeoplePanel() {
  const b = useB();
  const { wsId, me } = useBoardCtx();
  const presence = useBoard((s) => s.presence);
  const ui = useUI();
  const admin = isAdmin(b);
  const { list, reload } = useAccess(wsId, admin);
  const [invites, setInvites] = useState<Invite[]>([]);
  useEffect(() => {
    if (!admin) return;
    api<{ invites: Invite[] }>("GET", `/orgs/${b.workspace.org_id}/invites`).then((r) => setInvites(r.invites ?? []), () => {});
  }, [admin, b.workspace.org_id]);
  const online = new Set(presence.map((p) => p.id));
  const teamName = (id: string) => (visibleMember(b, id)?.name ?? "a manager") + "’s team";
  const accessText = (a: Access) =>
    a.role === "admin" ? "Full access" : [...a.manager_ids.map(teamName), ...(a.independent_staff ? ["independent staff"] : [])].join(", ").replace(/^/, "Can see ") || "No access yet";
  const remove = async (a: Access) => {
    try {
      await api("DELETE", `/workspaces/${wsId}/access/${a.user_id}`);
      ui.toast(`${a.name} no longer has access to ${b.workspace.name}.`, "ok");
      reload();
    } catch (e) {
      ui.toast(e instanceof ApiError ? e.message : "Couldn’t remove access.", "error");
    }
  };
  const pending = invites.filter((i) => i.workspaces?.some((g) => g.workspace_id === wsId));

  return (
    <>
      <PanelHead><h2>People and access</h2><span className="hint">Humans who can see {b.workspace.name}</span></PanelHead>
      <PanelBody>
        {!admin && (
          <>
            <p className="hint">Only admins can see and change who has access.</p>
            <div className="sb-sec">
              <span className="lbl">On the board now</span>
              {presence.map((p) => (
                <div className="trow" style={{ cursor: "default" }} key={p.id}>
                  <Avatar name={p.name} color={p.avatar_color} size={30} /><span className="spacer"><b>{p.name}{p.id === me ? " (you)" : ""}</b></span><span className="dot done" />
                </div>
              ))}
            </div>
          </>
        )}
        {admin && (
          <div className="stack-sm">
            {list === null && <p className="hint">Loading…</p>}
            {list?.map((a) => (
              <div className="trow" style={{ cursor: "default", alignItems: "flex-start" }} key={a.user_id}>
                <Avatar name={a.name} color={a.avatar_color} size={30} />
                <span className="spacer" style={{ minWidth: 0 }}>
                  <b>{a.name}{a.user_id === me ? " (you)" : ""}</b> <span className={`pill ${a.role === "admin" ? "ok" : ""}`}>{a.role === "admin" ? "Admin" : "Staff"}</span><br />
                  <span className="hint">{a.email}{online.has(a.user_id) ? ", on the board now" : ""}</span><br />
                  <span style={{ fontSize: 12 }}>{accessText(a)}</span>
                  {a.user_id !== me && (
                    <span className="row" style={{ marginTop: 6, gap: 6 }}>
                      <button className="btn btn-sm" onClick={() => ui.openDialog({ kind: "access", userId: a.user_id })}>Edit access</button>
                      <button className="btn btn-sm btn-danger" onClick={() => remove(a)}>Remove</button>
                    </span>
                  )}
                </span>
                {online.has(a.user_id) && <span className="dot done" style={{ marginTop: 8 }} />}
              </div>
            ))}
            {list && !list.length && <p className="hint">Only organization owners and admins can see this workspace so far.</p>}
            {pending.map((i) => (
              <div className="trow" style={{ cursor: "default", alignItems: "flex-start" }} key={i.id}>
                <span className="avatar" style={{ background: "var(--line)", width: 30, height: 30, fontSize: 10 }}>{i.email.slice(0, 2).toUpperCase()}</span>
                <span className="spacer" style={{ minWidth: 0 }}>
                  <b>{i.email}</b> <span className="pill warn">Invited</span><br />
                  <span style={{ fontSize: 12 }}>{i.workspaces?.find((g) => g.workspace_id === wsId)?.role === "admin" ? "Full access" : "Can see " + (i.workspaces?.find((g) => g.workspace_id === wsId)?.manager_ids ?? []).map(teamName).join(", ")}</span>
                </span>
              </div>
            ))}
            <span className="hint">Managers: {managers(b).length}. Organization owners and admins always see every workspace.</span>
          </div>
        )}
      </PanelBody>
      <PanelFoot>
        {admin ? <button className="btn btn-sm btn-primary" onClick={() => ui.openDialog({ kind: "invite" })}>+ Invite someone</button> : <span className="hint">Only admins can invite people.</span>}
      </PanelFoot>
    </>
  );
}
