"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Modal } from "@/components/Modal";
import { api, ApiError, formatDate, hardNavigate, ROLE_LABEL, type AdminUserDetail } from "@/lib/api";
import { useMe } from "@/lib/me";
import { usePathSegment } from "@/lib/path";

export function UserDetail() {
  const id = usePathSegment("/admin/users/");
  const { me } = useMe();
  const [data, setData] = useState<AdminUserDetail | null>(null);
  const [missing, setMissing] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  useEffect(() => {
    api<AdminUserDetail>("GET", "/admin/users/" + id).then(setData, (e) => e instanceof ApiError && e.status === 404 && setMissing(true));
  }, [id]);

  async function reset() {
    setBusy(true);
    setError("");
    try {
      await api("POST", `/admin/users/${id}/two-factor/reset`);
      if (id === me.user.id) return hardNavigate("/login");
      setConfirm(false);
      setDone(true);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Something went wrong. Try again.");
    }
    setBusy(false);
  }

  if (missing) return <><h1>Person not found</h1><Link href="/admin/users">All users</Link></>;
  if (!data) return <p className="sr-only" role="status">Loading…</p>;
  const u = data.user;
  return (
    <>
      <div className="stack-sm">
        <Link href="/admin/users">← Users</Link>
        <h1>{u.name}{u.is_hub_admin && <> <span className="pill">Hub admin</span></>}</h1>
        <p className="muted">{u.email} · Joined {formatDate(u.created_at)}</p>
      </div>
      {done && <div className="banner ok" role="status">Two-step sign-in was reset. {u.name} was signed out everywhere and will set it up again at next sign-in.</div>}
      <section className="card panel" aria-labelledby="ts-h">
        <h2 id="ts-h">Two-step sign-in</h2>
        <p className="muted">If {u.name} lost their phone and their recovery codes, reset it here. We email them when you do.</p>
        <div><button type="button" className="btn" onClick={() => { setConfirm(true); setError(""); }}>Reset two-step sign-in</button></div>
      </section>
      <section className="card" aria-labelledby="orgs-h" style={{ overflowX: "auto" }}>
        <h2 id="orgs-h" style={{ padding: "16px 16px 0" }}>Organizations ({data.organizations.length})</h2>
        <table className="table">
          <thead><tr><th>Name</th><th>Role</th></tr></thead>
          <tbody>
            {data.organizations.map((o) => (
              <tr key={o.id}>
                <td><Link href={`/admin/organizations/${o.id}`}>{o.name}</Link></td>
                <td><span className="pill">{ROLE_LABEL[o.role]}</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      {confirm && (
        <Modal title="Reset two-step sign-in?" onClose={() => !busy && setConfirm(false)}>
          <p className="lead">
            {u.name} will be signed out everywhere and will set up two-step sign-in again at next sign-in. Their old authenticator app and recovery codes stop working.
          </p>
          {error && <div className="banner bad" role="alert">{error}</div>}
          <div className="row">
            <span className="spacer" />
            <button type="button" className="btn" disabled={busy} onClick={() => setConfirm(false)}>Cancel</button>
            <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void reset()}>{busy ? "Resetting…" : "Reset"}</button>
          </div>
        </Modal>
      )}
    </>
  );
}
