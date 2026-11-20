"use client";

import type { SetupPerson } from "@/lib/setup/state";
import type { StepProps } from "../Setup";

export type OrgPerson = { id: string; name: string; email: string; role: string };

/** Invite people: teammates already in the organization, and new people by email. */
export function People({ p, set, people, orgName, errors }: StepProps) {
  const managers = p.members.filter((m) => m.kind === "manager");
  const rows = p.people;
  const setRow = (i: number, patch: Partial<SetupPerson>) => set((x) => ({ ...x, people: x.people.map((r, k) => (k === i ? { ...r, ...patch } : r)) }));
  const included = (id: string) => rows.findIndex((r) => r.user_id === id);
  const toggleExisting = (o: OrgPerson, on: boolean) =>
    set((x) => ({ ...x, people: on ? [...x.people, { user_id: o.id, email: o.email, name: o.name, role: "staff", manager_keys: managers.slice(0, 1).map((m) => m.key), independent_staff: false }] : x.people.filter((r) => r.user_id !== o.id) }));
  const access = (r: SetupPerson, i: number) => r.role === "admin" ? <span className="hint">Full access to this workspace and its settings.</span> : (
    <>
      <span className="hint">Can access:</span>
      {managers.map((m) => (
        <label key={m.key}><input type="checkbox" checked={r.manager_keys.includes(m.key)} onChange={(e) => setRow(i, { manager_keys: e.target.checked ? [...r.manager_keys, m.key] : r.manager_keys.filter((k) => k !== m.key) })} />{m.name || "Manager"} and their staff</label>
      ))}
      <label><input type="checkbox" checked={r.independent_staff} onChange={(e) => setRow(i, { independent_staff: e.target.checked })} />Independent staff</label>
    </>
  );
  const role = (r: SetupPerson, i: number) => (
    <div className="seg" role="group" aria-label="Role">
      <button type="button" aria-pressed={r.role === "admin"} onClick={() => setRow(i, { role: "admin" })}>Admin</button>
      <button type="button" aria-pressed={r.role === "staff"} onClick={() => setRow(i, { role: "staff" })}>Staff</button>
    </div>
  );
  return (
    <>
      <h1>Invite people to {orgName}</h1>
      <p className="lead">People join {orgName} and get access to this workspace. <b>Admins</b> have full access. <b>Staff</b> only see the managers and independent staff you allow. Each person connects their own computer when they join, and their goals run there with their own sign-in.</p>
      {people.length > 0 && (
        <>
          <h3 style={{ fontSize: 14 }}>People in {orgName}</h3>
          {people.map((o) => {
            const i = included(o.id);
            return (
              <div className="inv" key={o.id}>
                <label className="toggle" style={{ fontWeight: 600 }}><input type="checkbox" checked={i >= 0} onChange={(e) => toggleExisting(o, e.target.checked)} /> {o.name} <span className="hint" style={{ fontWeight: 400 }}>{o.email}</span></label>
                {i >= 0 && <>{role(rows[i], i)}<span /><div className="acc">{access(rows[i], i)}</div></>}
              </div>
            );
          })}
        </>
      )}
      <h3 style={{ fontSize: 14, marginTop: 10 }}>Invite someone new</h3>
      {rows.map((r, i) => (r.user_id ? null : (
        <div className="inv" key={i}>
          <label className="field"><span className="lbl">Email</span>
            <input className="input" type="email" value={r.email} onChange={(e) => setRow(i, { email: e.target.value })} placeholder="alex@company.com" aria-invalid={!!errors[`people.${i}.email`]} />
            {errors[`people.${i}.email`] && <span className="err">{errors[`people.${i}.email`]}</span>}
          </label>
          {role(r, i)}
          <button type="button" className="icon-btn" onClick={() => set((x) => ({ ...x, people: x.people.filter((_, k) => k !== i) }))} aria-label="Remove invite">✕</button>
          <div className="acc">{access(r, i)}</div>
        </div>
      )))}
      {!rows.some((r) => !r.user_id) && <p className="hint">No one yet. You can invite people any time from organization settings.</p>}
      <div><button type="button" className="btn" onClick={() => set((x) => ({ ...x, people: [...x.people, { user_id: null, email: "", role: "staff", manager_keys: managers.slice(0, 1).map((m) => m.key), independent_staff: false }] }))}>+ Invite someone</button></div>
    </>
  );
}
