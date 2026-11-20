"use client";

import { useEffect, useState } from "react";
import { LogoUpload } from "@/components/LogoUpload";
import { Denied, Section } from "@/components/PageShell";
import { api, ApiError, currentOrg, isOrgAdmin, ROLE_LABEL, type OrgResponse } from "@/lib/api";
import { useMe } from "@/lib/me";

const ZONES = ["UTC", "Asia/Kathmandu", "Asia/Kolkata", "Europe/London", "Europe/Berlin", "America/New_York", "America/Los_Angeles", "Asia/Tokyo", "Australia/Sydney"];

/** /org/general: the organization's name, limits for each person and the owner's actions. */
export default function OrgGeneralPage() {
  const { me, refresh } = useMe();
  const summary = currentOrg(me);
  const [org, setOrg] = useState<OrgResponse["organization"] | null>(null);
  const [name, setName] = useState("");
  const [limits, setLimits] = useState({ session: "", week: "", tz: "UTC" });
  const [msg, setMsg] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!summary) return;
    api<OrgResponse>("GET", "/orgs/" + summary.id).then(({ organization: o }) => {
      setOrg(o);
      setName(o.name);
      setLimits({ session: String(o.default_session_limit), week: String(o.default_weekly_limit), tz: o.timezone });
    });
  }, [summary]);
  if (!summary || !isOrgAdmin(summary.role)) return <Denied title="Organization" />;

  const patch = async (body: Record<string, unknown>, key: string) => {
    setMsg({});
    try {
      const r = await api<OrgResponse>("PATCH", "/orgs/" + summary.id, body);
      setOrg(r.organization);
      setMsg({ [key]: "Saved" });
      await refresh();
    } catch (e) {
      setMsg({ [key]: e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : "Something went wrong." });
    }
  };
  const num = (s: string) => Number(s.replace(/\D/g, ""));

  return (
    <>
      <h1>General</h1>
      <p className="lead">{org?.name ?? summary.name}. Your role: {ROLE_LABEL[summary.role]}.</p>
      <Section title="Organization">
        <form className="row wrap" style={{ alignItems: "flex-end" }} onSubmit={(e) => { e.preventDefault(); patch({ name }, "name"); }}>
          <label className="field spacer"><span>Name</span><input className="input" value={name} onChange={(e) => setName(e.target.value)} required /></label>
          <button className="btn">Save</button>
        </form>
        <LogoUpload base={"/orgs/" + summary.id} name={org?.name ?? summary.name} url={org?.logo_url} onChange={async () => { setOrg((await api<OrgResponse>("GET", "/orgs/" + summary.id)).organization); await refresh(); }} />
        {msg.name && <span className={msg.name === "Saved" ? "hint" : "err"} role="status">{msg.name}</span>}
        <dl className="kv">
          <dt>Address</dt><dd><code>{org?.slug}</code></dd>
          <dt>Created</dt><dd>{org ? new Date(org.created_at).toLocaleDateString() : ""}</dd>
        </dl>
      </Section>
      <Section title="Limits for each person" sub="Canopy’s own limits, separate from what your CLI accounts allow. At 100%, a person’s new goals and chats wait until the window resets.">
        <form className="stack-sm" onSubmit={(e) => { e.preventDefault(); patch({ default_session_limit: num(limits.session), default_weekly_limit: num(limits.week), timezone: limits.tz }, "limits"); }}>
          <div className="row wrap">
            <label className="field spacer"><span>Per 5-hour session (tokens)</span><input className="input tnum" inputMode="numeric" value={limits.session} onChange={(e) => setLimits({ ...limits, session: e.target.value })} /></label>
            <label className="field spacer"><span>Per week (tokens)</span><input className="input tnum" inputMode="numeric" value={limits.week} onChange={(e) => setLimits({ ...limits, week: e.target.value })} /></label>
          </div>
          <label className="field"><span>Time zone</span>
            <select className="input" value={limits.tz} onChange={(e) => setLimits({ ...limits, tz: e.target.value })}>
              {[...new Set([limits.tz, ...ZONES])].map((z) => <option key={z} value={z}>{z}</option>)}
            </select>
            <span className="hint">Weeks reset Monday 00:00 in this time zone. Set personal overrides on each person’s usage page.</span>
          </label>
          <div className="row"><button className="btn btn-primary">Save limits</button>{msg.limits && <span className={msg.limits === "Saved" ? "hint" : "err"} role="status">{msg.limits}</span>}</div>
        </form>
      </Section>
      {summary.role === "owner" && <OwnerActions orgId={summary.id} orgName={org?.name ?? ""} />}
    </>
  );
}

type Person = { id: string; name: string; role: string };

function OwnerActions({ orgId, orgName }: { orgId: string; orgName: string }) {
  const [people, setPeople] = useState<Person[]>([]);
  const [to, setTo] = useState("");
  const [confirm, setConfirm] = useState("");
  const [msg, setMsg] = useState("");
  useEffect(() => {
    api<{ people: Person[] }>("GET", `/orgs/${orgId}/people`).then((r) => {
      const admins = r.people.filter((p) => p.role === "admin");
      setPeople(admins);
      setTo(admins[0]?.id ?? "");
    });
  }, [orgId]);
  const run = async (fn: () => Promise<unknown>, after: string) => {
    try {
      await fn();
      location.assign(after);
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Something went wrong.");
    }
  };
  return (
    <Section title="Owner actions" sub="Only the owner can do these." danger>
      <form className="row wrap" style={{ alignItems: "flex-end" }} onSubmit={(e) => { e.preventDefault(); run(() => api("POST", `/orgs/${orgId}/transfer`, { user_id: to }), "/org/general"); }}>
        <label className="field spacer"><span>Transfer ownership to</span>
          <select className="input" value={to} onChange={(e) => setTo(e.target.value)} disabled={!people.length}>
            {people.length ? people.map((p) => <option key={p.id} value={p.id}>{p.name} (admin)</option>) : <option>Make someone an admin first</option>}
          </select></label>
        <button className="btn" disabled={!to}>Transfer</button>
      </form>
      <form className="row wrap" style={{ alignItems: "flex-end" }} onSubmit={(e) => { e.preventDefault(); run(() => api("DELETE", `/orgs/${orgId}`, { confirm }), "/"); }}>
        <label className="field spacer"><span>Type <b>{orgName}</b> to delete the organization</span>
          <input className="input" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="off" /></label>
        <button className="btn btn-danger" disabled={confirm !== orgName}>Delete organization</button>
      </form>
      {msg && <span className="err">{msg}</span>}
    </Section>
  );
}
