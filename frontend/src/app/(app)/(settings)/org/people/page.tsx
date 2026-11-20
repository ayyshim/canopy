"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { Avatar } from "@/components/board/icons";
import { Denied, Section } from "@/components/PageShell";
import { api, ApiError, currentOrg, isOrgAdmin } from "@/lib/api";
import { ago } from "@/lib/board/derive";
import { useMe } from "@/lib/me";
import type { MemberView, WorkspaceSummary } from "@/lib/types";

type Person = { id: string; name: string; email: string; avatar_color: string; role: string; workspaces: { id: string; name: string; role: string }[] };
type Invite = { id: string; email: string; role: string; inviter_name: string; created_at: string; expires_at: string; workspaces: { workspace_id: string; role: string; manager_ids: string[] }[] };
type UsageRow = { id: string; usage: { week_used: number; week_limit: number } };

const meterClass = (p: number) => (p >= 90 ? "red" : p >= 70 ? "amber" : "");

/** /org/people: everyone in the organization, their role and what they can see; invitations. */
export default function PeoplePage() {
  const { me } = useMe();
  const org = currentOrg(me);
  const [people, setPeople] = useState<Person[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [usage, setUsage] = useState<Record<string, UsageRow["usage"]>>({});
  const [error, setError] = useState("");
  const load = useCallback(() => {
    if (!org) return;
    api<{ people: Person[] }>("GET", `/orgs/${org.id}/people`).then((r) => setPeople(r.people));
    api<{ invites: Invite[] }>("GET", `/orgs/${org.id}/invites`).then((r) => setInvites(r.invites), () => {});
    api<{ people: UsageRow[] }>("GET", `/orgs/${org.id}/usage`).then((r) => setUsage(Object.fromEntries(r.people.map((p) => [p.id, p.usage]))), () => {});
  }, [org]);
  useEffect(load, [load]);
  if (!org || !isOrgAdmin(org.role)) return <Denied title="People" />;

  const act = async (fn: () => Promise<unknown>) => {
    setError("");
    try {
      await fn();
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Something went wrong.");
    }
  };

  return (
    <>
      <h1>People</h1>
      <p className="lead">Everyone in {org.name}, their role and what they can see.</p>
      <InviteForm orgId={org.id} onSent={load} />
      {error && <div className="banner bad">{error}</div>}
      <section className="card panel">
        <h2>Members</h2>
        <div className="scroll-x">
          <table className="table">
            <thead><tr><th>Person</th><th>Role</th><th>Workspaces</th><th>Usage this week</th><th /></tr></thead>
            <tbody>
              {people.map((p) => {
                const u = usage[p.id];
                const wp = u ? Math.min(100, (u.week_used / Math.max(1, u.week_limit)) * 100) : 0;
                return (
                  <tr key={p.id}>
                    <td><div className="row"><Avatar name={p.name} color={p.avatar_color} size={28} /><span>{p.name}<br /><span className="hint">{p.email}</span></span></div></td>
                    <td>
                      {p.role === "owner" || p.id === me.user.id ? <span className="pill">{p.role === "owner" ? "Owner" : p.role === "admin" ? "Admin" : "Member"}</span> : (
                        <select className="input" style={{ minHeight: 36, width: "auto" }} value={p.role}
                          onChange={(e) => act(() => api("PATCH", `/orgs/${org.id}/memberships/${p.id}`, { role: e.target.value }))}>
                          <option value="admin">Admin</option><option value="member">Member</option>
                        </select>
                      )}
                    </td>
                    <td>
                      {p.workspaces.length ? p.workspaces.map((w) => <span key={w.id} className={`pill ${w.role === "admin" ? "ok" : ""}`} style={{ marginRight: 4 }}>{w.name} · {w.role === "admin" ? "Admin" : "Staff"}</span>)
                        : <span className="muted">{p.role === "member" ? "No workspace" : "Every workspace"}</span>}
                    </td>
                    <td style={{ minWidth: 120 }}>
                      <div className={`meter ${meterClass(wp)}`}><i style={{ width: `${wp}%` }} /></div>
                      <span className="hint tnum">{Math.round(wp)}% of weekly limit</span>
                    </td>
                    <td>
                      <div className="row" style={{ justifyContent: "flex-end" }}>
                        <Link className="btn btn-sm" href={`/org/usage/${p.id}`}>Details</Link>
                        {p.role !== "owner" && p.id !== me.user.id && (
                          <button className="btn btn-sm btn-danger" onClick={() => act(() => api("DELETE", `/orgs/${org.id}/memberships/${p.id}`))}>Remove</button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
      <section className="card panel">
        <h2>Invitations</h2>
        {invites.length ? (
          <div className="scroll-x">
            <table className="table">
              <thead><tr><th>Email</th><th>Role</th><th>Workspaces</th><th>Sent</th><th /></tr></thead>
              <tbody>
                {invites.map((i) => (
                  <tr key={i.id}>
                    <td>{i.email}</td><td>{i.role === "admin" ? "Admin" : "Member"}</td>
                    <td>{i.workspaces.length ? i.workspaces.map((w) => (w.role === "admin" ? "Admin" : "Staff")).join(", ") : "No workspace"}</td>
                    <td>{ago(i.created_at)}{i.inviter_name ? ` by ${i.inviter_name}` : ""}</td>
                    <td><div className="row" style={{ justifyContent: "flex-end" }}>
                      <button className="btn btn-sm btn-danger" onClick={() => act(() => api("DELETE", `/orgs/${org.id}/invites/${i.id}`))}>Revoke</button>
                    </div></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <p className="hint">No open invitations.</p>}
        <span className="hint">Invitations expire after 7 days.</span>
      </section>
    </>
  );
}

function InviteForm({ orgId, onSent }: { orgId: string; onSent: () => void }) {
  const [workspaces, setWorkspaces] = useState<WorkspaceSummary[]>([]);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("member");
  const [ws, setWs] = useState("");
  const [wsRole, setWsRole] = useState("staff");
  const [found, setFound] = useState<{ ws: string; managers: MemberView[] }>({ ws: "", managers: [] });
  const managers = found.ws === ws ? found.managers : [];
  const [teams, setTeams] = useState<string[]>([]);
  const [msg, setMsg] = useState<{ text: string; ok: boolean } | null>(null);
  useEffect(() => {
    api<{ workspaces: WorkspaceSummary[] }>("GET", `/orgs/${orgId}/workspaces`).then((r) => { setWorkspaces(r.workspaces); setWs(r.workspaces[0]?.id ?? ""); });
  }, [orgId]);
  useEffect(() => {
    if (!ws) return;
    api<{ members: MemberView[] }>("GET", `/workspaces/${ws}`).then((r) => setFound({ ws, managers: r.members.filter((m) => m.kind === "manager" && m.name) }));
  }, [ws]);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setMsg(null);
    const grants = ws && wsRole !== "none" ? [{ workspace_id: ws, role: wsRole, manager_ids: wsRole === "staff" ? teams.filter((t) => t !== "pool") : [], independent_staff: teams.includes("pool") }] : [];
    try {
      const r = await api<{ link: string }>("POST", `/orgs/${orgId}/invites`, { email, role, workspace_grants: grants });
      setMsg({ text: `Invite sent to ${email}. Link: ${r.link}`, ok: true });
      setEmail("");
      onSent();
    } catch (err) {
      setMsg({ text: err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : "Couldn’t send the invite.", ok: false });
    }
  };
  return (
    <Section title="Invite someone" sub="They get an email with a link. People can watch the board, chat with members and act on what needs them, within the access you give.">
      <form className="stack-sm" onSubmit={submit}>
        <div className="row wrap" style={{ alignItems: "flex-end" }}>
          <label className="field spacer"><span>Email</span><input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="alex@company.com" required /></label>
          <label className="field"><span>Organization role</span>
            <select className="input" value={role} onChange={(e) => setRole(e.target.value)}><option value="member">Member</option><option value="admin">Admin</option></select></label>
          {workspaces.length > 0 && (
            <>
              <label className="field"><span>Workspace</span>
                <select className="input" value={ws} onChange={(e) => setWs(e.target.value)}>{workspaces.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label>
              <label className="field"><span>Role there</span>
                <select className="input" value={wsRole} onChange={(e) => setWsRole(e.target.value)}><option value="staff">Staff</option><option value="admin">Admin</option><option value="none">No access</option></select></label>
            </>
          )}
        </div>
        {wsRole === "staff" && ws && (
          <div className="row wrap" style={{ gap: 16 }}>
            <span className="hint">Staff can see</span>
            {managers.map((m) => (
              <label className="toggle" key={m.id}><input type="checkbox" checked={teams.includes(m.id)} onChange={(e) => setTeams(e.target.checked ? [...teams, m.id] : teams.filter((t) => t !== m.id))} /> {m.name}’s team</label>
            ))}
            <label className="toggle"><input type="checkbox" checked={teams.includes("pool")} onChange={(e) => setTeams(e.target.checked ? [...teams, "pool"] : teams.filter((t) => t !== "pool"))} /> Independent staff</label>
          </div>
        )}
        <div className="row"><button className="btn btn-primary">Send invite</button>{msg && <span className={msg.ok ? "hint" : "err"}>{msg.text}</span>}</div>
      </form>
    </Section>
  );
}
