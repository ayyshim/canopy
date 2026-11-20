"use client";

import { useCallback, useEffect, useState } from "react";
import { Section } from "@/components/PageShell";
import { api, ApiError, currentOrg, isOrgAdmin } from "@/lib/api";
import { ago, fmtTokens } from "@/lib/board/derive";
import { useMe } from "@/lib/me";
import type { AccountView, Computer } from "@/lib/types";
import { PairComputer } from "./PairComputer";

type Runners = { runners: Computer[]; my_computer: string | null; computers_by_person: Record<string, string | null> };
type Person = { id: string; name: string };

/** /org/computers: paired computers, which one runs your goals, sharing, and how each signs in. */
export default function ComputersPage() {
  const { me } = useMe();
  const org = currentOrg(me);
  const admin = isOrgAdmin(org?.role);
  const [data, setData] = useState<Runners | null>(null);
  const [accounts, setAccounts] = useState<AccountView[]>([]);
  const [people, setPeople] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const load = useCallback(() => {
    if (!org) return;
    api<Runners>("GET", `/orgs/${org.id}/runners`).then(setData);
    api<{ connections: AccountView[] }>("GET", `/orgs/${org.id}/connections`).then((r) => setAccounts(r.connections));
    if (admin) api<{ people: Person[] }>("GET", `/orgs/${org.id}/people`).then((r) => setPeople(Object.fromEntries(r.people.map((p) => [p.id, p.name]))), () => {});
  }, [org, admin]);
  useEffect(load, [load]);
  if (!org) return null;

  const act = async (fn: () => Promise<unknown>) => {
    setError("");
    try {
      await fn();
      load();
    } catch (e) {
      setError(e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : "Something went wrong.");
    }
  };
  const visible = (data?.runners ?? []).filter((r) => admin || r.owner_user_id === me.user.id || r.shared);
  const mine = data?.my_computer ? data.runners.find((r) => r.id === data.my_computer) : undefined;
  const shared = data?.runners.find((r) => r.shared);
  const ownerName = (id: string) => (id === me.user.id ? me.user.name : people[id] ?? "Someone");

  return (
    <>
      <h1>{admin ? "Computers and sign-ins" : "Your computers"}</h1>
      <p className="lead">Computers paired with this hub. Goals run on the computer of the person who gives them, signed in with that computer’s own CLI account. A shared computer runs goals for people who don’t have one.</p>
      {!mine && (
        <div className="note-box" style={{ background: "var(--amber-soft)" }}>
          You don’t have a computer connected. {shared ? <>Your goals and chats run on <b>{shared.name}</b>, the shared computer, until you connect one.</> : "Your goals wait until you connect one."}
        </div>
      )}
      {error && <div className="banner bad">{error}</div>}
      {visible.map((r) => (
        <ComputerCard key={r.id} r={r} account={accounts.find((a) => a.runner_id === r.id && a.cli === "claude")} mine={data?.my_computer === r.id}
          own={r.owner_user_id === me.user.id} admin={admin} owner={ownerName(r.owner_user_id)} act={act} orgId={org.id} />
      ))}
      <Section title={mine ? "Pair another computer" : "Connect your computer"} sub="Run this on the computer you want to add. It pairs with your account, and your goals can run there with that computer’s CLI sign-in.">
        <PairComputer orgId={org.id} onPaired={load} />
      </Section>
    </>
  );
}

function ComputerCard({ r, account, mine, own, admin, owner, act, orgId }: {
  r: Computer; account?: AccountView; mine: boolean; own: boolean; admin: boolean; owner: string; orgId: string;
  act: (fn: () => Promise<unknown>) => void;
}) {
  const canEdit = own || (admin && r.shared) || admin;
  const [name, setName] = useState(r.name);
  const [mode, setMode] = useState(account?.mode ?? "subscription");
  const [key, setKey] = useState("");
  const [cap, setCap] = useState(account?.cap_tokens_per_day ? String(account.cap_tokens_per_day) : "");
  const claude = (r.clis as { cli: string; installed: boolean; version: string; logged_in: boolean }[] | undefined)?.find((c) => c.cli === "claude");
  const api_ = mode === "api_key";
  const pct = account?.cap_tokens_per_day ? Math.min(100, (account.used_tokens / account.cap_tokens_per_day) * 100) : 0;
  const saveSignIn = () => act(() => api("PUT", `/runners/${r.id}/connections/claude`, {
    enabled: true, mode, api_key: key || undefined, cap_tokens_per_day: api_ && cap ? Number(cap.replace(/\D/g, "")) : null,
  }).then(() => setKey("")));

  return (
    <section className="card panel">
      <div className="row wrap">
        <span className={`dot ${r.online ? "done" : "need"}`} />
        {canEdit ? (
          <input className="input spacer" style={{ fontWeight: 700, fontSize: 16, minHeight: 36 }} value={name} onChange={(e) => setName(e.target.value)}
            onBlur={() => name.trim() && name !== r.name && act(() => api("PATCH", `/runners/${r.id}`, { name: name.trim() }))} aria-label="Computer name" />
        ) : <h2 className="spacer">{r.name}</h2>}
        {r.shared && <span className="pill info">Shared</span>}
        {mine && <span className="pill ok">Runs your goals</span>}
        <span className={`pill ${r.online ? "ok" : "warn"}`}>{r.online ? "Connected" : "Offline"}</span>
      </div>
      <dl className="kv">
        <dt>Owner</dt><dd>{owner}{r.shared ? " (shared with everyone in the organization)" : ""}</dd>
        <dt>Host</dt><dd className="mono">{r.hostname || "—"} {r.os && `· ${r.os}`}</dd>
        <dt>Client</dt><dd>canopy {r.version || "?"} · sandbox: {r.sandbox || "?"}</dd>
        <dt>Claude Code</dt><dd>{claude?.installed ? `Installed${claude.version ? " " + claude.version : ""}${claude.logged_in ? ", signed in" : ""}` : "Not found on this computer"}</dd>
        <dt>Last seen</dt><dd>{r.online ? "Now" : r.last_seen_at ? ago(r.last_seen_at) : "Never"}</dd>
      </dl>
      <div className="stack-sm">
        <span className="lbl">How Claude Code signs in here</span>
        <div className="seg" role="group" aria-label={`Sign-in mode on ${r.name}`}>
          <button aria-pressed={!api_} disabled={!canEdit || r.shared} onClick={() => setMode("subscription")}>Claude subscription</button>
          <button aria-pressed={api_} disabled={!canEdit} onClick={() => setMode("api_key")}>Anthropic API key</button>
        </div>
        {api_ ? (
          <>
            <div className="row wrap" style={{ alignItems: "flex-end" }}>
              <label className="field spacer"><span>API key {account?.key_fingerprint && <span className="hint">(saved: {account.key_fingerprint})</span>}</span>
                <input className="input mono" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder={account?.key_fingerprint ? "Leave empty to keep it" : "sk-ant-…"} disabled={!canEdit} autoComplete="off" /></label>
              <label className="field"><span>Daily cap (tokens)</span>
                <input className="input tnum" inputMode="numeric" value={cap} onChange={(e) => setCap(e.target.value)} placeholder="No cap" disabled={!canEdit} /></label>
            </div>
            <span className="hint">The key goes straight to Claude Code’s config on {r.name}. The hub keeps only its last four characters.</span>
            {account?.cap_tokens_per_day ? (
              <div className="stack-sm" style={{ gap: 4 }}>
                <div className="row"><span className="hint">Used today</span><span className="spacer" /><span className="tnum">{fmtTokens(account.used_tokens)} of {fmtTokens(account.cap_tokens_per_day)} tokens</span></div>
                <div className={`meter ${pct >= 100 ? "red" : pct >= 80 ? "amber" : ""}`}><i style={{ width: `${pct}%` }} /></div>
              </div>
            ) : null}
          </>
        ) : (
          <p className="hint">Canopy can’t see how much of a subscription is left; it shows when Claude Code reports a limit and when it resets. A subscription is personal, so it only runs its owner’s goals.</p>
        )}
        {account?.limited_until && new Date(account.limited_until) > new Date() && <span className="pill warn">Usage limit until {new Date(account.limited_until).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>}
        {canEdit && <div><button className="btn btn-sm" onClick={saveSignIn}>Save sign-in</button></div>}
      </div>
      <div className="row wrap">
        {own && !r.shared && !mine && <button className="btn btn-sm" onClick={() => act(() => api("PUT", `/orgs/${orgId}/my-computer`, { runner_id: r.id }))}>Run my goals here</button>}
        {!own && r.shared && !mine && <button className="btn btn-sm" onClick={() => act(() => api("PUT", `/orgs/${orgId}/my-computer`, { runner_id: r.id }))}>Run my goals here</button>}
        {admin && (
          <button className="btn btn-sm" onClick={() => act(() => api("PATCH", `/runners/${r.id}`, { shared: !r.shared }))}
            disabled={!r.shared && account?.mode !== "api_key"} title={!r.shared && account?.mode !== "api_key" ? "Only a computer signed in with an API key can be shared: a subscription is personal" : undefined}>
            {r.shared ? "Stop sharing" : "Share with everyone"}
          </button>
        )}
        {(own || admin) && <button className="btn btn-sm btn-danger" onClick={() => act(() => api("DELETE", `/runners/${r.id}`))}>Remove</button>}
      </div>
    </section>
  );
}
