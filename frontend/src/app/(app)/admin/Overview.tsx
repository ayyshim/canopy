"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { api, formatDate, timeAgo, type AdminOverview } from "@/lib/api";
import { useMe } from "@/lib/me";

const TILES: { key: keyof AdminOverview["totals"]; label: string; sub?: (t: AdminOverview["totals"]) => string }[] = [
  { key: "organizations", label: "Organizations", sub: (t) => `${t.organizations_7d} new this week` },
  { key: "users", label: "People", sub: (t) => `${t.users_7d} new this week` },
  { key: "active_users_24h", label: "Active in the last day" },
  { key: "hub_admins", label: "Hub admins" },
];

export function Overview() {
  const { me } = useMe();
  const [data, setData] = useState<AdminOverview | null>(null);
  useEffect(() => {
    api<AdminOverview>("GET", "/admin/overview").then(setData);
  }, []);
  if (!data) return <p className="sr-only" role="status">Loading…</p>;
  return (
    <>
      <div className="stack-sm">
        <h1>{data.hub.name}</h1>
        <p className="muted">
          Set up {formatDate(data.hub.set_up_at)} · Sign-ups {data.hub.signup_open ? "open" : "closed"} ·{" "}
          <Link href="/admin/settings">Hub settings</Link>
        </p>
      </div>
      {me.organizations.length === 0 && (
        <div className="banner">
          <div className="row wrap">
            <span className="spacer">You’re not in an organization yet. Create one to set up workspaces and run agents.</span>
            <Link className="btn btn-primary btn-sm" href="/new-org">Create an organization</Link>
          </div>
        </div>
      )}
      <div className="tiles">
        {TILES.map((t) => (
          <div key={t.key} className="card tile">
            <span className="muted">{t.label}</span>
            <strong className="tile-num">{data.totals[t.key].toLocaleString()}</strong>
            {t.sub && <span className="muted">{t.sub(data.totals)}</span>}
          </div>
        ))}
      </div>
      <p className="muted">Token usage, sessions and agent activity across the hub appear here once agents are running.</p>
      <section className="card panel" aria-labelledby="events-h">
        <h2 id="events-h">Recent hub activity</h2>
        {data.recent_events.length === 0 ? (
          <p className="muted">Nothing yet.</p>
        ) : (
          <div>
            {data.recent_events.map((e, i) => (
              <div key={i} className="trow">
                <span className="spacer">{e.summary}</span>
                <span className="muted">{timeAgo(e.at)}</span>
              </div>
            ))}
          </div>
        )}
      </section>
    </>
  );
}
