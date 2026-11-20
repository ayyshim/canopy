"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { api, ApiError, formatDate, ROLE_LABEL, type AdminOrgDetail } from "@/lib/api";
import { usePathSegment } from "@/lib/path";

export function OrgDetail() {
  const id = usePathSegment("/admin/organizations/");
  const [data, setData] = useState<AdminOrgDetail | null>(null);
  const [missing, setMissing] = useState(false);
  useEffect(() => {
    api<AdminOrgDetail>("GET", "/admin/organizations/" + id).then(setData, (e) => e instanceof ApiError && e.status === 404 && setMissing(true));
  }, [id]);

  if (missing) return <><h1>Organization not found</h1><Link href="/admin/organizations">All organizations</Link></>;
  if (!data) return <p className="sr-only" role="status">Loading…</p>;
  const o = data.organization;
  return (
    <>
      <div className="stack-sm">
        <Link href="/admin/organizations">← Organizations</Link>
        <h1>{o.name}</h1>
        <p className="muted"><code>{o.slug}</code> · Created {formatDate(o.created_at)}{o.about ? ` · ${o.about}` : ""}</p>
      </div>
      <div className="card panel">
        <div className="row"><span className="spacer">Session limit per person</span><span>{o.default_session_limit.toLocaleString()} tokens / 5 h</span></div>
        <div className="row"><span className="spacer">Weekly limit per person</span><span>{o.default_weekly_limit.toLocaleString()} tokens</span></div>
        <div className="row"><span className="spacer">Time zone</span><span>{o.timezone}</span></div>
      </div>
      <section className="card" aria-labelledby="members-h" style={{ overflowX: "auto" }}>
        <h2 id="members-h" style={{ padding: "16px 16px 0" }}>Members ({data.members.length})</h2>
        <table className="table">
          <thead><tr><th>Name</th><th>Email</th><th>Role</th><th>Joined</th></tr></thead>
          <tbody>
            {data.members.map((m) => (
              <tr key={m.id}>
                <td><Link href={`/admin/users?q=${encodeURIComponent(m.email)}`}>{m.name}</Link></td>
                <td>{m.email}</td>
                <td><span className="pill">{ROLE_LABEL[m.role]}</span>{m.status === "invited" && <span className="muted"> invited</span>}</td>
                <td>{formatDate(m.joined_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </>
  );
}
