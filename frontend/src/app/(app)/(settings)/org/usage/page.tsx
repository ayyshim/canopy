"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Avatar } from "@/components/board/icons";
import { Card, StatTile, StatTiles, TabPanel, Tabs, useTab } from "@/components/ui";
import { Bars, DayBars, Heatmap, meterClass } from "@/components/UsageCharts";
import { api, currentOrg, isOrgAdmin } from "@/lib/api";
import { ago, fmtTokens } from "@/lib/board/derive";
import { useMe } from "@/lib/me";
import type { AccountView } from "@/lib/types";

type Usage = { session_used: number; session_limit: number; session_reset_at: string; week_used: number; week_limit: number; week_reset_at: string };
type OrgUsage = {
  tiles: { tokens: number; on_board: number; sessions_running: number; goals_completed: number; decisions: number; waiting_on_people: number; avg_answer_seconds: number; outputs: number };
  near_limit: { id: string; name: string; which: string; percent: number }[];
  tokens_per_day: { day: string; by_workspace: Record<string, number> | null }[];
  workspaces: Record<string, string>; today: string;
  people: { id: string; name: string; avatar_color: string; role: string; usage: Usage; tokens: number; goals: number; decisions: number; last_active_at?: string }[];
  models: { model: string; tokens: number }[]; heatmap: number[][];
  agents: { id: string; name: string; workspace: string; model: string; status: string; tokens: number; sessions: number; tasks_done: number; errors: number }[];
  accounts: AccountView[]; recent: { id: string; at: string; summary: string; actor?: string }[];
};

const pct = (a: number, b: number) => Math.min(100, (a / Math.max(1, b)) * 100);

/** /org/usage: tokens, people against their limits, agents and accounts. Members go to their own page. */
export default function UsagePage() {
  const { me } = useMe();
  const org = currentOrg(me);
  const admin = isOrgAdmin(org?.role);
  const [week, setWeek] = useState("this");
  const [ws, setWs] = useState("");
  const [data, setData] = useState<OrgUsage | null>(null);
  const [tab, setTab] = useTab("org-usage", "overview", ["overview", "people", "agents", "accounts"]);
  useEffect(() => {
    if (!org || !admin) return;
    api<OrgUsage>("GET", `/orgs/${org.id}/usage?week=${week}${ws ? "&workspace=" + ws : ""}`).then(setData);
  }, [org, admin, week, ws]);
  useEffect(() => {
    if (org && !admin) location.replace(`/org/usage/${me.user.id}`);
  }, [org, admin, me.user.id]);
  if (!org || !admin) return null;
  if (!data) return <p className="hint">Loading…</p>;
  const t = data.tiles;

  return (
    <>
      <div className="row wrap">
        <div className="stack-sm"><h1>Usage and analytics</h1><p className="muted">Tokens, limits and activity for {org.name}.</p></div><span className="spacer" />
        <div className="seg" role="group" aria-label="Week">
          <button aria-pressed={week === "this"} onClick={() => setWeek("this")}>This week</button>
          <button aria-pressed={week === "last"} onClick={() => setWeek("last")}>Last week</button>
        </div>
        <div className="seg" role="group" aria-label="Workspace">
          <button aria-pressed={!ws} onClick={() => setWs("")}>All workspaces</button>
          {Object.entries(data.workspaces).map(([id, name]) => <button key={id} aria-pressed={ws === id} onClick={() => setWs(id)}>{name}</button>)}
        </div>
      </div>
      {data.near_limit.length > 0 && (
        <div className="banner warn">{data.near_limit.map((n) => `${n.name} is at ${n.percent}% of their ${n.which === "session" ? "5-hour" : "weekly"} limit`).join(" · ")}. New goals and chats from them queue at 100%.</div>
      )}
      <Tabs label="Usage sections" idBase="ou" value={tab} onChange={setTab} tabs={[
        { id: "overview", label: "Overview" }, { id: "people", label: "People", count: data.people.length },
        { id: "agents", label: "Agents", count: data.agents.length }, { id: "accounts", label: "Sign-ins and activity" },
      ]} />
      <TabPanel id="overview" value={tab} idBase="ou"><div className="stack">
      <StatTiles>
        {[["Tokens", fmtTokens(t.tokens), "this period"], ["On the board now", t.on_board, "people"], ["Sessions running", t.sessions_running, "agent CLIs"],
          ["Goals completed", t.goals_completed, `with ${t.decisions} human decisions`], ["Waiting on people", t.waiting_on_people, `avg answer ${Math.round(t.avg_answer_seconds / 60)} min`], ["Outputs made", t.outputs, "this period"]]
          .map(([l, v, s]) => <StatTile key={l as string} label={l as string} value={v} sub={s as string} />)}
      </StatTiles>
      <div className="grid2">
        <Card title="Tokens per day"><DayBars days={data.tokens_per_day} names={data.workspaces} today={data.today} /></Card>
        <Card title="Tokens by model"><Bars rows={data.models.map((m) => ({ label: m.model, value: m.tokens }))} />
          <h2>Busiest hours</h2><Heatmap cells={data.heatmap} />
        </Card>
      </div>
      </div></TabPanel>
      <TabPanel id="people" value={tab} idBase="ou">
      <div className="card">
        <div className="scroll-x">
          <table className="table stack-m">
            <thead><tr><th>Person</th><th>Current session (5 h)</th><th>This week</th><th>Tokens</th><th>Goals</th><th>Decisions</th><th>Last active</th></tr></thead>
            <tbody>
              {data.people.map((p) => {
                const sp = pct(p.usage.session_used, p.usage.session_limit), wp = pct(p.usage.week_used, p.usage.week_limit);
                return (
                  <tr key={p.id}>
                    <td><Link className="row" href={`/org/usage/${p.id}`} style={{ color: "inherit", textDecoration: "none" }}><Avatar name={p.name} color={p.avatar_color} size={24} /><span>{p.name}<br /><span className="muted" style={{ fontSize: 12 }}>{p.role === "owner" ? "Owner" : p.role === "admin" ? "Admin" : "Member"}</span></span></Link></td>
                    <td data-l="Session (5 h)" style={{ minWidth: 150 }}><div className={`meter ${meterClass(sp)}`}><i style={{ width: `${sp}%` }} /></div><span className="muted tnum" style={{ fontSize: 11.5 }}>{Math.round(sp)}%</span></td>
                    <td data-l="This week" style={{ minWidth: 130 }}><div className={`meter ${meterClass(wp)}`}><i style={{ width: `${wp}%` }} /></div><span className="muted tnum" style={{ fontSize: 11.5 }}>{Math.round(wp)}% · resets Monday</span></td>
                    <td className="tnum" data-l="Tokens">{fmtTokens(p.tokens ?? 0)}</td><td className="tnum" data-l="Goals">{p.goals}</td><td className="tnum" data-l="Decisions">{p.decisions}</td><td data-l="Last active">{p.last_active_at ? ago(p.last_active_at) : "—"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
      </TabPanel>
      <TabPanel id="agents" value={tab} idBase="ou">
      <div className="card">
        <div className="scroll-x">
          <table className="table stack-m">
            <thead><tr><th>Agent</th><th>Workspace</th><th>Model</th><th>Status</th><th>Tokens</th><th>Sessions</th><th>Tasks done</th><th>Errors</th></tr></thead>
            <tbody>
              {data.agents.map((a) => (
                <tr key={a.id}><td>{a.name}</td><td data-l="Workspace">{a.workspace}</td><td data-l="Model">{a.model}</td><td data-l="Status">{a.status.replace("_", " ")}</td>
                  <td className="tnum" data-l="Tokens">{fmtTokens(a.tokens ?? 0)}</td><td className="tnum" data-l="Sessions">{a.sessions}</td><td className="tnum" data-l="Tasks done">{a.tasks_done}</td><td className="tnum" data-l="Errors">{a.errors}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      </TabPanel>
      <TabPanel id="accounts" value={tab} idBase="ou">
      <div className="grid2">
        <Card title="Sign-ins and caps">
          {data.accounts.map((a) => (
            <div className="row" style={{ fontSize: 13 }} key={a.runner_id + a.cli}>
              <span className={`dot ${a.paused || (a.limited_until && new Date(a.limited_until) > new Date()) ? "need" : "done"}`} />
              <b className="spacer">{a.cli}</b><span className="hint">{a.mode === "api_key" ? "API key" : "Subscription"}</span>
              {a.mode === "api_key" && a.cap_tokens_per_day ? <span className={`pill ${a.paused ? "bad" : a.used_tokens / a.cap_tokens_per_day >= 0.8 ? "warn" : ""}`}>{Math.round(pct(a.used_tokens, a.cap_tokens_per_day))}% of cap</span> : <span className="pill ok">Available</span>}
            </div>
          ))}
          <span className="hint">Each person’s goals use their own computer’s sign-in. Provider limits pause only the work on that computer. <Link className="linkbtn" href="/org/computers">Manage</Link></span>
        </Card>
        <Card title="Recent activity" action={<Link className="btn btn-sm" href="/org/activity">Full log</Link>}>
          {data.recent.map((a) => <div className="row" style={{ fontSize: 13 }} key={a.id}><span className="spacer">{a.actor ? `${a.actor}: ` : ""}{a.summary}</span><span className="muted" style={{ fontSize: 12 }}>{ago(a.at)}</span></div>)}
        </Card>
      </div>
      </TabPanel>
    </>
  );
}
