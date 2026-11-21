"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Card, EmptyState, StatTile, StatTiles, TabPanel, Tabs, useTab } from "@/components/ui";
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
  const [tab, setTab] = useTab("hub-overview", "overview", ["overview", "activity"]);
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
      <Tabs label="Hub sections" idBase="ho" value={tab} onChange={setTab}
        tabs={[{ id: "overview", label: "Overview" }, { id: "activity", label: "Recent activity", count: data.recent_events.length }]} />
      <TabPanel id="overview" value={tab} idBase="ho">
        <div className="stack">
          <StatTiles>
            {TILES.map((t) => <StatTile key={t.key} label={t.label} value={data.totals[t.key].toLocaleString()} sub={t.sub?.(data.totals)} />)}
          </StatTiles>
          <p className="hint">Token usage and cost across the hub appear here once agents are running.</p>
        </div>
      </TabPanel>
      <TabPanel id="activity" value={tab} idBase="ho">
        <Card>
          {data.recent_events.length === 0 ? (
            <EmptyState icon="◷">Nothing has happened on this hub yet.</EmptyState>
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
        </Card>
      </TabPanel>
    </>
  );
}
