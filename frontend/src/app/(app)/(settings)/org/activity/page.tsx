"use client";

import { useEffect, useState } from "react";
import { Denied } from "@/components/PageShell";
import { api, currentOrg, isOrgAdmin } from "@/lib/api";
import { ago } from "@/lib/board/derive";
import { useMe } from "@/lib/me";

type Row = { id: string; at: string; type: string; summary: string; actor?: string; workspace?: string };
const FILTERS = ["everything", "goals", "approvals", "answers", "reviews", "access", "people", "chats"];

/** /org/activity: the organization's audit log, filterable and exportable as CSV. */
export default function ActivityPage() {
  const { me } = useMe();
  const org = currentOrg(me);
  const [filter, setFilter] = useState("everything");
  const [rows, setRows] = useState<Row[]>([]);
  useEffect(() => {
    if (org && isOrgAdmin(org.role)) api<{ activity: Row[] }>("GET", `/orgs/${org.id}/activity?limit=200${filter === "everything" ? "" : "&type=" + filter}`).then((r) => setRows(r.activity));
  }, [org, filter]);
  if (!org || !isOrgAdmin(org.role)) return <Denied title="Activity log" />;
  return (
    <>
      <div className="row wrap"><h1>Activity log</h1><span className="spacer" />
        <a className="btn btn-sm" href={`/api/v1/orgs/${org.id}/activity?format=csv${filter === "everything" ? "" : "&type=" + filter}`}>Download CSV</a></div>
      <div className="row wrap">
        {FILTERS.map((f) => <button key={f} className="chip-f" aria-pressed={filter === f} onClick={() => setFilter(f)}>{f[0].toUpperCase() + f.slice(1)}</button>)}
      </div>
      <div className="card scroll-x">
        <table className="table">
          <thead><tr><th>When</th><th>Person</th><th>What happened</th><th>Where</th></tr></thead>
          <tbody>
            {rows.map((a) => <tr key={a.id}><td className="tnum" style={{ whiteSpace: "nowrap" }}>{ago(a.at)}</td><td>{a.actor ?? "Canopy"}</td><td>{a.summary}</td><td>{a.workspace ?? "Organization"}</td></tr>)}
            {!rows.length && <tr><td colSpan={4} className="muted">Nothing yet.</td></tr>}
          </tbody>
        </table>
      </div>
      <span className="muted" style={{ fontSize: 12 }}>Kept 400 days.</span>
    </>
  );
}
