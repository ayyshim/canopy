"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { managers, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import { useB, useBoardCtx } from "../context";
import { useAccess } from "../panels/PeoplePanel";

/** AccessDialog invites someone into the workspace, or changes what an existing person can see. */
export function AccessDialog({ userId }: { userId?: string }) {
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const { list } = useAccess(wsId, true);
  const current = userId ? list?.find((a) => a.user_id === userId) : undefined;
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"admin" | "staff" | null>(null);
  const [teams, setTeams] = useState<string[] | null>(null);
  const [error, setError] = useState("");
  const r = role ?? current?.role ?? "staff";
  const t = teams ?? [...(current?.manager_ids ?? []), ...(current?.independent_staff ? ["pool"] : [])];
  const toggle = (id: string, on: boolean) => setTeams(on ? [...t, id] : t.filter((x) => x !== id));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const grant = { role: r, manager_ids: r === "staff" ? t.filter((x) => x !== "pool") : [], independent_staff: r === "staff" && t.includes("pool") };
    try {
      if (userId) {
        await api("PUT", `/workspaces/${wsId}/access/${userId}`, grant);
        ui.toast(`Access updated for ${current?.name ?? "them"}.`, "ok");
      } else {
        await api("POST", `/orgs/${b.workspace.org_id}/invites`, { email, role: "member", workspace_grants: [{ workspace_id: wsId, ...grant }] });
        ui.toast(`Invite sent to ${email}. They’ll show up on the board when they join.`, "ok");
      }
      ui.openDialog(null);
    } catch (err) {
      setError(err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : "Couldn’t save.");
    }
  };

  return (
    <form className="dialog" onSubmit={submit}>
      <h2>{userId ? `Edit access for ${current?.name ?? "…"}` : "Invite someone"}</h2>
      <p>People can watch the board, chat with members and act on what needs them, within the access you give.</p>
      <label className="field"><span>Email</span>
        {userId ? <input className="input" value={current?.email ?? ""} disabled /> :
          <input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required placeholder="alex@company.com" autoComplete="off" autoFocus />}
      </label>
      <div className="field"><span>Role</span>
        <div className="seg" role="group" aria-label="Role">
          <button type="button" aria-pressed={r === "admin"} onClick={() => setRole("admin")}>Admin</button>
          <button type="button" aria-pressed={r === "staff"} onClick={() => setRole("staff")}>Staff</button>
        </div>
        <span className="hint">{r === "admin" ? "Admins can see and change everything in this workspace, including members and people." : "Staff see only the parts you choose. They can chat with those members and act on what needs them."}</span>
      </div>
      {r === "staff" && (
        <div className="field"><span>Can access</span>
          <div className="chks">
            {managers(b).map((m) => (
              <label key={m.id}><input type="checkbox" checked={t.includes(m.id)} onChange={(e) => toggle(m.id, e.target.checked)} />{visibleMember(b, m.id)?.name ?? "A manager"} and their staff</label>
            ))}
            <label><input type="checkbox" checked={t.includes("pool")} onChange={(e) => toggle("pool", e.target.checked)} />Independent staff</label>
          </div>
        </div>
      )}
      {!userId && <p className="hint">They connect their own computer when they join; their goals run there with their own sign-in.</p>}
      {error && <span className="err">{error}</span>}
      <div className="row"><span className="spacer" />
        <button type="button" className="btn" onClick={() => ui.openDialog(null)}>Cancel</button>
        <button className="btn btn-primary">{userId ? "Save" : "Send invite"}</button>
      </div>
    </form>
  );
}
