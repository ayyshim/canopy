"use client";

import Link from "next/link";
import { useState } from "react";
import { Avatar } from "@/components/board/icons";
import { Field } from "@/components/Field";
import { PauseNotifications } from "@/components/PauseNotifications";
import { Section } from "@/components/PageShell";
import { PushDevice } from "@/components/PushDevice";
import { api, ApiError, hardNavigate, ROLE_LABEL, type Me } from "@/lib/api";
import { useMe } from "@/lib/me";
import { setTheme, useTheme, type Theme } from "@/lib/theme";

const PREFS: [string, string][] = [["questions", "Questions and approvals"], ["progress", "Progress updates"], ["errors", "Errors"], ["goal_ready", "Goal ready"], ["daily", "Daily summary"]];

/** /account: profile, theme, notifications, password, organizations. */
export function Account() {
  const { me, setMe } = useMe();
  const [name, setName] = useState(me.user.name);
  const [nameMsg, setNameMsg] = useState("");
  const [pw, setPw] = useState({ current_password: "", new_password: "" });
  const [pwErrors, setPwErrors] = useState<Record<string, string>>({});
  const [pwSaved, setPwSaved] = useState(false);
  const prefs = me.user.notifications;
  const [prefMsg, setPrefMsg] = useState("");

  const theme = useTheme();

  async function saveName(ev: React.FormEvent) {
    ev.preventDefault();
    try {
      setMe(await api<Me>("PATCH", "/me", { name }));
      setNameMsg("Saved");
    } catch (e) {
      setNameMsg(e instanceof ApiError ? (e.fields.name ?? e.message) : "Something went wrong.");
    }
  }

  async function savePassword(ev: React.FormEvent) {
    ev.preventDefault();
    setPwErrors({});
    setPwSaved(false);
    try {
      const body = me.user.has_password ? pw : { new_password: pw.new_password };
      setMe(await api<Me>("PATCH", "/me", body));
      setPw({ current_password: "", new_password: "" });
      setPwSaved(true);
    } catch (e) {
      setPwErrors(e instanceof ApiError ? e.fields : { new_password: "Something went wrong." });
    }
  }

  async function setPref(key: string, channel: "desktop" | "push" | "email", on: boolean) {
    setPrefMsg("");
    try {
      setMe(await api<Me>("PATCH", "/me", { notifications: { [key]: { ...prefs[key], [channel]: on } } }));
    } catch {
      setPrefMsg("Couldn’t save. Try again.");
      return;
    }
    if (channel === "desktop" && on && "Notification" in window && Notification.permission === "default") {
      if ((await Notification.requestPermission()) === "denied") setPrefMsg("Your browser blocks notifications from Canopy. Allow them in the site settings.");
    }
  }


  async function logoutEverywhere() {
    await api("POST", "/auth/logout", { everywhere: true }).catch(() => {});
    hardNavigate("/login");
  }

  return (
    <>
      <h1>Account and notifications</h1>
      <p className="lead">Signed in as {me.user.name}.</p>
      <Section title="Profile">
        <form className="stack-sm" onSubmit={saveName} noValidate>
          <div className="row wrap" style={{ alignItems: "flex-end" }}>
            <Avatar name={me.user.name} color={me.user.avatar_color} size={40} />
            <div className="spacer">
              <Field label="Name" error={nameMsg && nameMsg !== "Saved" ? nameMsg : undefined}>
                {(p) => <input {...p} className="input" value={name} onChange={(e) => { setName(e.target.value); setNameMsg(""); }} />}
              </Field>
            </div>
            <label className="field spacer"><span>Email</span><input className="input" type="email" value={me.user.email} disabled /></label>
          </div>
          <div className="row"><button className="btn" type="submit" disabled={name === me.user.name}>Save</button>{nameMsg === "Saved" && <span className="hint" role="status">Saved</span>}</div>
        </form>
      </Section>
      <Section title="Theme">
        <div className="seg" role="group" aria-label="Theme">
          {(["system", "light", "dark"] as Theme[]).map((t) => <button key={t} aria-pressed={theme === t} onClick={() => setTheme(t)}>{t[0].toUpperCase() + t.slice(1)}</button>)}
        </div>
      </Section>
      <Section title="Notifications" sub="When something needs you and you’re not on the board.">
        <table className="table" style={{ maxWidth: 520 }}>
          <thead><tr><th /><th>Desktop</th><th>Push</th><th>Email</th></tr></thead>
          <tbody>
            {PREFS.map(([k, label]) => (
              <tr key={k}>
                <td>{label}</td>
                {(["desktop", "push", "email"] as const).map((c) => (
                  <td key={c}>
                    {k === "daily" && c !== "email" ? <span className="muted" aria-label="Not available">—</span> : (
                      <label className="toggle"><input type="checkbox" checked={prefs[k]?.[c] ?? false} onChange={(e) => setPref(k, c, e.target.checked)} /> <span className="sr">{label} by {c}</span></label>
                    )}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {prefMsg && <span className="err" role="status">{prefMsg}</span>}
        <PushDevice />
        <span className="hint">Desktop notifications show while Canopy is open in a background tab. Push reaches you when Canopy is closed, on each device where you turn it on. Progress updates are off until you choose them. Emails go out when something has waited two minutes. The daily summary arrives each morning with goals that moved, decisions waiting and your usage.</span>
      </Section>
      <Section title="Pause notifications" sub="Stops push for a while. Email and Discord carry on.">
        <PauseNotifications />
      </Section>
      <Section title="Password" sub="Changing it signs you out everywhere else.">
        <form className="row wrap" style={{ alignItems: "flex-end" }} onSubmit={savePassword} noValidate>
          {me.user.has_password && (
            <div className="spacer"><Field label="Current password" error={pwErrors.current_password}>
              {(p) => <input {...p} className="input" type="password" autoComplete="current-password" value={pw.current_password} onChange={(e) => setPw({ ...pw, current_password: e.target.value })} />}
            </Field></div>
          )}
          <div className="spacer"><Field label="New password" error={pwErrors.new_password}>
            {(p) => <input {...p} className="input" type="password" autoComplete="new-password" value={pw.new_password} onChange={(e) => setPw({ ...pw, new_password: e.target.value })} />}
          </Field></div>
          <button className="btn" type="submit" disabled={!pw.new_password}>Change password</button>
        </form>
        {pwSaved && <span className="hint" role="status">Password changed</span>}
      </Section>
      <Section title="Organizations">
        {me.organizations.map((o) => (
          <div className="trow" style={{ cursor: "default" }} key={o.id}>
            <span className="spacer">{o.name}</span>
            <span className="pill">{ROLE_LABEL[o.role]}</span>
            {o.id === me.current_org_id ? <span className="pill ok">Current</span> : (
              <button className="btn btn-sm" onClick={async () => { setMe(await api<Me>("PATCH", "/me", { last_org_id: o.id })); hardNavigate("/board"); }}>Switch</button>
            )}
          </div>
        ))}
        <div><Link className="btn btn-sm" href="/new-org">+ Create an organization</Link></div>
      </Section>
      <div className="row">
        <Link className="btn" href={`/org/usage/${me.user.id}`}>Your usage</Link>
        <Link className="btn" href="/org/computers">Your computers</Link>
        <span className="spacer" />
        <button className="btn btn-ghost" onClick={logoutEverywhere}>Log out everywhere</button>
      </div>
    </>
  );
}
