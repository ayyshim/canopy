"use client";

import { useCallback, useEffect, useState } from "react";
import { Section } from "@/components/PageShell";
import { api, ApiError } from "@/lib/api";

type Discord = { configured: boolean; webhook: string; kinds: Record<string, boolean>; role_id: string; forum: boolean; last_ok_at: string | null; last_error: string };

const KINDS: [string, string][] = [["questions", "Questions and approvals"], ["progress", "Progress updates"], ["goal_ready", "Goal ready or done"], ["errors", "Errors"]];

const when = (iso: string) => new Date(iso).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });

/** Workspace settings, Notifications tab: where this workspace’s notifications go in Discord. */
export function Notifications({ wsId }: { wsId: string }) {
  const [d, setD] = useState<Discord | null>(null);
  const [hook, setHook] = useState("");
  const [replacing, setReplacing] = useState(false);
  const [kinds, setKinds] = useState<Record<string, boolean>>({});
  const [role, setRole] = useState("");
  const [forum, setForum] = useState(false);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  const apply = useCallback((x: Discord) => {
    setD(x);
    setKinds(x.kinds);
    setRole(x.role_id);
    setForum(x.forum);
  }, []);
  const load = useCallback(() => api<{ discord: Discord }>("GET", `/workspaces/${wsId}/discord`).then((r) => apply(r.discord), () => setMsg("Couldn’t load the Discord settings.")), [wsId, apply]);
  useEffect(() => { void load(); }, [load]);

  async function act(fn: () => Promise<string | void>) {
    setBusy(true);
    setMsg("");
    setErr({});
    try {
      const done = await fn();
      if (done) setMsg(done);
    } catch (e) {
      if (e instanceof ApiError && Object.keys(e.fields).length) setErr(e.fields);
      else setMsg(e instanceof ApiError ? e.message : "Something went wrong. Try again.");
    }
    setBusy(false);
  }

  const save = (ev: React.FormEvent) => {
    ev.preventDefault();
    return act(async () => {
      const body: Record<string, unknown> = { kinds, role_id: role.trim(), forum };
      if (!d?.configured || replacing) body.webhook = hook.trim();
      apply((await api<{ discord: Discord }>("PUT", `/workspaces/${wsId}/discord`, body)).discord);
      setHook("");
      setReplacing(false);
      return "Saved";
    });
  };

  const remove = () => act(async () => {
    if (!window.confirm("Stop sending notifications to Discord? Canopy forgets the webhook.")) return;
    apply((await api<{ discord: Discord }>("DELETE", `/workspaces/${wsId}/discord`)).discord);
    return "Discord is off for this workspace.";
  });

  const test = () => act(async () => {
    await api("POST", `/workspaces/${wsId}/discord/test`);
    setTimeout(load, 6000);
    return "Test message queued. It should appear in Discord in a few seconds.";
  });

  if (!d) return <><h1>Notifications</h1><p className="hint">{msg || "Loading…"}</p></>;
  const showHook = !d.configured || replacing;
  return (
    <>
      <h1>Notifications</h1>
      <p className="lead">Choose where this workspace’s notifications go. Each person sets their own push and email on their account page.</p>
      <Section title="Discord" sub="Canopy posts to one Discord channel for this workspace. Messages are named after the workspace and the agent.">
        <form className="stack-sm" onSubmit={save} noValidate>
          {showHook ? (
            <label className="field">
              <span>Webhook URL</span>
              <input className="input" value={hook} onChange={(e) => setHook(e.target.value)} placeholder="https://discord.com/api/webhooks/…" autoComplete="off" spellCheck={false} aria-invalid={!!err.webhook} />
              {err.webhook && <span className="err" role="alert">{err.webhook}</span>}
            </label>
          ) : (
            <div className="field">
              <span>Webhook URL</span>
              <div className="row wrap">
                <input className="input spacer" value={d.webhook} readOnly aria-label="Saved webhook, hidden" />
                <button type="button" className="btn btn-sm" onClick={() => setReplacing(true)}>Replace</button>
                <button type="button" className="btn btn-sm" onClick={remove} disabled={busy}>Remove</button>
              </div>
              <span className="hint">The secret part is kept private. Replace it to use another channel.</span>
            </div>
          )}
          {replacing && <div><button type="button" className="btn btn-sm btn-ghost" onClick={() => { setReplacing(false); setHook(""); }}>Keep the saved one</button></div>}
          {!d.configured && (
            <details>
              <summary>How to get a webhook</summary>
              <ol className="hint" style={{ paddingLeft: 20, margin: "6px 0 0" }}>
                <li>In Discord, open the channel’s settings (the gear next to its name).</li>
                <li>Choose Integrations, then Webhooks, then New webhook.</li>
                <li>Choose Copy webhook URL and paste it here.</li>
              </ol>
            </details>
          )}
          <fieldset className="field" style={{ border: 0, padding: 0 }}>
            <legend>Send to Discord</legend>
            {KINDS.map(([k, label]) => (
              <label className="toggle" key={k}><input type="checkbox" checked={kinds[k] ?? true} onChange={(e) => setKinds({ ...kinds, [k]: e.target.checked })} /> {label}</label>
            ))}
          </fieldset>
          <label className="field">
            <span>Role to ping for questions</span>
            <input className="input" inputMode="numeric" value={role} onChange={(e) => setRole(e.target.value)} placeholder="Optional role ID" aria-invalid={!!err.role_id} />
            {err.role_id ? <span className="err" role="alert">{err.role_id}</span> : <span className="hint">Only questions and approvals ping the role. In Discord, turn on developer mode, then right-click the role and choose Copy role ID.</span>}
          </label>
          <label className="toggle"><input type="checkbox" checked={forum} onChange={(e) => setForum(e.target.checked)} /> One thread per goal</label>
          <span className="hint" style={{ marginTop: -6 }}>For forum channels. A goal’s first message starts a thread, and later messages go in it.</span>
          <div className="row wrap">
            <button className="btn btn-primary" type="submit" disabled={busy || (showHook && !hook.trim())}>Save</button>
            {d.configured && <button type="button" className="btn" onClick={test} disabled={busy}>Send a test</button>}
            {msg && <span className="hint" role="status">{msg}</span>}
          </div>
        </form>
        {d.configured && (
          <p className={d.last_error ? "err" : "hint"} role="status">
            {d.last_error ? `Last problem: ${d.last_error}` : d.last_ok_at ? `Last message sent ${when(d.last_ok_at)}.` : "Nothing sent yet."}
          </p>
        )}
      </Section>
    </>
  );
}
