"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { Avatar } from "@/components/board/icons";
import { Section } from "@/components/PageShell";
import { Bars, DayBars, Heatmap, meterClass } from "@/components/UsageCharts";
import { api, ApiError, currentOrg, isOrgAdmin } from "@/lib/api";
import { ago, fmtTokens } from "@/lib/board/derive";
import { useMe } from "@/lib/me";

type Data = {
  person: { id: string; name: string; email: string; avatar_color: string; role: string };
  usage: { session_used: number; session_limit: number; session_reset_at: string; week_used: number; week_limit: number; week_reset_at: string };
  overrides: { session: number | null; week: number | null };
  change: { this_period: number; previous_period: number };
  tiles: { tokens: number; goals: number; messages: number; decisions: number; reviews: number; avg_answer_seconds: number };
  tokens_per_day: { day: string; by_workspace: Record<string, number> | null }[]; workspaces: Record<string, string>;
  sessions: { started: string; ended: string; tokens: number; workspace: string; agent: string; live: boolean }[];
  agents: { member_id: string; name: string; tokens: number }[]; heatmap: number[][];
  recent: { id: string; at: string; summary: string }[];
};

const pct = (a: number, b: number) => Math.min(100, (a / Math.max(1, b)) * 100);
const until = (iso: string) => {
  const m = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60000));
  return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`;
};

/** /org/usage/<person>: one person's usage, limits and activity. Members may open only their own. */
export function PersonUsage() {
  const { me } = useMe();
  const org = currentOrg(me);
  const admin = isOrgAdmin(org?.role);
  const id = usePathname().split("/")[3] ?? "";
  const [week, setWeek] = useState("this");
  const [data, setData] = useState<Data | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    if (!org || !id || id === "_") return;
    api<Data>("GET", `/orgs/${org.id}/usage/people/${id}?week=${week}`).then(setData, (e) => setError(e instanceof ApiError ? e.message : "Couldn’t load usage."));
  }, [org, id, week]);
  useEffect(load, [load]);
  if (error) return <><h1>Usage</h1><div className="note-box">{error}</div></>;
  if (!data) return <p className="hint">Loading…</p>;
  const u = data.usage, p = data.person;
  const sp = pct(u.session_used, u.session_limit), wp = pct(u.week_used, u.week_limit);
  const change = data.change.previous_period ? Math.round(((data.change.this_period - data.change.previous_period) / data.change.previous_period) * 100) : 0;

  return (
    <>
      {admin && <div className="row"><Link className="linkbtn" href="/org/usage">← Usage and analytics</Link></div>}
      <div className="row wrap">
        <Avatar name={p.name} color={p.avatar_color} size={44} />
        <div className="spacer"><h1>{p.name}</h1><p className="hint">{p.role === "owner" ? "Owner" : p.role === "admin" ? "Admin" : "Member"} · {p.email}</p></div>
        <div className="seg" role="group" aria-label="Week">
          <button aria-pressed={week === "this"} onClick={() => setWeek("this")}>This week</button>
          <button aria-pressed={week === "last"} onClick={() => setWeek("last")}>Last week</button>
        </div>
      </div>
      <div className="grid2">
        {([["Current session (5 h)", sp, `resets in ${until(u.session_reset_at)}`, u.session_limit, data.overrides.session], ["This week", wp, `resets ${new Date(u.week_reset_at).toLocaleString([], { weekday: "long", hour: "2-digit", minute: "2-digit" })}`, u.week_limit, data.overrides.week]] as const).map(([l, v, r, lim, o]) => (
          <section className="card panel" key={l}>
            <div className="row"><h2 className="spacer">{l}</h2><b className="tnum">{Math.round(v)}%</b></div>
            <div className={`meter ${meterClass(v)}`}><i style={{ width: `${v}%` }} /></div>
            <span className="hint">{r} · {o ? `personal limit ${o.toLocaleString()}` : `limit ${lim.toLocaleString()}`} tokens{change ? ` · ${change > 0 ? "up" : "down"} ${Math.abs(change)}% on last week` : ""}</span>
          </section>
        ))}
      </div>
      <div className="tiles">
        {[["Tokens", fmtTokens(data.tiles.tokens)], ["Goals given", data.tiles.goals], ["Messages to agents", data.tiles.messages], ["Decisions", data.tiles.decisions],
          ["Outputs reviewed", data.tiles.reviews], ["Average answer", `${Math.round(data.tiles.avg_answer_seconds / 60)} min`]]
          .map(([l, v]) => <div className="tile" key={l as string}><span className="muted">{l}</span><b>{v}</b></div>)}
      </div>
      <div className="grid2">
        <section className="card panel"><h2>Tokens per day</h2><DayBars days={data.tokens_per_day} names={data.workspaces} /></section>
        <section className="card panel"><h2>Where the tokens went</h2><Bars rows={data.agents.map((a) => ({ label: a.name, value: a.tokens }))} /><h2 style={{ marginTop: 6 }}>Busiest hours</h2><Heatmap cells={data.heatmap} /></section>
      </div>
      <section className="card panel">
        <h2>Sessions</h2>
        <div className="scroll-x">
          <table className="table">
            <thead><tr><th>Started</th><th>Length</th><th>Workspace</th><th>Agent</th><th>Tokens</th></tr></thead>
            <tbody>
              {data.sessions.map((s, i) => (
                <tr key={i}>
                  <td>{s.live ? <span className="pill ok">Now</span> : new Date(s.started).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })}</td>
                  <td>{Math.max(1, Math.round((new Date(s.ended).getTime() - new Date(s.started).getTime()) / 60000))} min{s.live ? " so far" : ""}</td>
                  <td>{s.workspace}</td><td>{s.agent}</td><td className="tnum">{fmtTokens(s.tokens)}</td>
                </tr>
              ))}
              {!data.sessions.length && <tr><td colSpan={5} className="muted">No sessions in this period.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>
      {admin && <Overrides orgId={org!.id} personId={p.id} overrides={data.overrides} onSaved={load} />}
      <section className="card panel">
        <h2>Recent activity</h2>
        {data.recent.map((a) => <div className="row" style={{ fontSize: 13 }} key={a.id}><span className="spacer">{a.summary}</span><span className="hint">{ago(a.at)}</span></div>)}
        {!data.recent.length && <p className="hint">Nothing yet.</p>}
      </section>
    </>
  );
}

function Overrides({ orgId, personId, overrides, onSaved }: { orgId: string; personId: string; overrides: Data["overrides"]; onSaved: () => void }) {
  const [session, setSession] = useState(overrides.session ? String(overrides.session) : "");
  const [week, setWeek] = useState(overrides.week ? String(overrides.week) : "");
  const [msg, setMsg] = useState("");
  const num = (s: string) => (s.trim() ? Number(s.replace(/\D/g, "")) : null);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api("PATCH", `/orgs/${orgId}/memberships/${personId}`, { personal_session_limit: num(session), set_session_limit: true, personal_weekly_limit: num(week), set_weekly_limit: true });
      setMsg("Saved");
      onSaved();
    } catch (err) {
      setMsg(err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : "Couldn’t save.");
    }
  };
  return (
    <Section title="Personal limits" sub="Override the organization defaults for this person. Leave empty to use the defaults.">
      <form className="row wrap" style={{ alignItems: "flex-end" }} onSubmit={save}>
        <label className="field spacer"><span>Per 5-hour session</span><input className="input tnum" inputMode="numeric" value={session} onChange={(e) => setSession(e.target.value)} placeholder="Organization default" /></label>
        <label className="field spacer"><span>Per week</span><input className="input tnum" inputMode="numeric" value={week} onChange={(e) => setWeek(e.target.value)} placeholder="Organization default" /></label>
        <button className="btn">Save</button>
      </form>
      {msg && <span className={msg === "Saved" ? "hint" : "err"}>{msg}</span>}
    </Section>
  );
}
