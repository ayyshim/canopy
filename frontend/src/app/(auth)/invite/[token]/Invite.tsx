"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { api, ApiError, hardNavigate, type Me } from "@/lib/api";
import { pendingStep, twoFactorPath, type TwoFactorPending } from "@/lib/twoFactor";

type Info = {
  email: string; organization: string; inviter: string; role: string; account_exists: boolean;
  workspaces: { id: string; name: string; role: string; sees: string[] }[];
};

/** /invite/<token>: join an organization (and its workspaces) from an invitation. */
export function Invite() {
  const token = usePathname().split("/")[2] ?? "";
  const [info, setInfo] = useState<Info | null>(null);
  const [state, setState] = useState<"loading" | "open" | "invalid" | "declined" | "joined">("loading");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [peek, setPeek] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [me, setMe] = useState<Me | null>(null);

  useEffect(() => {
    if (!token || token === "_") return;
    api<Info>("GET", `/invites/${token}`).then((i) => { setInfo(i); setState("open"); }, () => setState("invalid"));
  }, [token]);

  const accept = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrors({});
    try {
      const res = await api<Me | TwoFactorPending>("POST", `/invites/${token}/accept`, { name, password });
      const step = pendingStep(res);
      if (step) {
        const ws = info?.workspaces[0];
        return hardNavigate(twoFactorPath(step, ws ? `/w/${ws.id}` : "/board"));
      }
      setMe(res as Me);
      setState("joined");
    } catch (err) {
      setErrors(err instanceof ApiError ? (Object.keys(err.fields).length ? err.fields : { form: err.message }) : { form: "Something went wrong. Try again." });
    }
  };
  const decline = async () => {
    await api("POST", `/invites/${token}/decline`).catch(() => {});
    setState("declined");
  };

  if (state === "loading") return <p className="hint">Loading…</p>;
  if (state === "invalid") {
    return (
      <div className="authcard"><h1>This invitation can’t be used</h1>
        <p className="lead">It may have expired or already been used. Ask the person who invited you to send a new one.</p>
        <Link className="btn btn-primary wide" href="/login">Sign in</Link></div>
    );
  }
  if (state === "declined") {
    return (
      <div className="authcard"><h1>Invitation declined</h1>
        <p className="lead">We’ve let {info?.inviter || "them"} know. You can close this page.</p>
        <Link className="btn wide" href="/login">Sign in instead</Link></div>
    );
  }
  if (state === "joined" && me) {
    const ws = info?.workspaces[0];
    return (
      <div className="authcard">
        <h1>You’re in, {me.user.name.split(" ")[0]}</h1>
        <p className="lead">You joined <b>{info?.organization}</b>{ws ? <> and the <b>{ws.name}</b> workspace as {ws.role === "admin" ? "an admin" : "a member"}</> : null}.</p>
        <div className="note-box">Your AI team works on your computer. You can connect it now or later.</div>
        <button className="btn btn-primary wide" onClick={() => hardNavigate(ws ? `/w/${ws.id}` : "/board")}>Open {ws?.name ?? "Canopy"}</button>
        <button className="btn wide" onClick={() => hardNavigate("/org/computers")}>Connect my computer</button>
      </div>
    );
  }
  return (
    <form className="authcard" onSubmit={accept} noValidate>
      <span className="hint">{info?.inviter || "Someone"} invited you</span>
      <h1>Join {info?.organization}</h1>
      <p className="lead">As {info?.role === "admin" ? "an admin" : "a member"}.</p>
      {info?.workspaces.map((w) => (
        <div className="box" key={w.id}>
          <dl className="kv"><dt>Workspace</dt><dd>{w.name}</dd><dt>Your role</dt><dd>{w.role === "admin" ? "Admin (can see and change everything)" : "Member"}</dd>
            {w.role === "staff" && <><dt>You can see</dt><dd>{w.sees.join(", ") || "Nothing yet"}</dd></>}</dl>
        </div>
      ))}
      {errors.form && <div className="banner bad">{errors.form}</div>}
      {!info?.account_exists && (
        <label className="field"><span>Your name</span><input className="input" value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" aria-invalid={!!errors.name} />{errors.name && <span className="err">{errors.name}</span>}</label>
      )}
      <label className="field"><span>Your email</span><input className="input" value={info?.email ?? ""} disabled /></label>
      <div className="field">
        <label className="lbl" htmlFor="inv-pw">Password</label>
        <div className="pw">
          <input className="input" id="inv-pw" type={peek ? "text" : "password"} value={password} onChange={(e) => setPassword(e.target.value)} autoComplete={info?.account_exists ? "current-password" : "new-password"} aria-invalid={!!errors.password} />
          <button type="button" onClick={() => setPeek(!peek)}>{peek ? "Hide" : "Show"}</button>
        </div>
        <span className="hint">{info?.account_exists ? "You already have an account. Enter your usual password." : "At least 8 characters."}</span>
        {errors.password && <span className="err">{errors.password}</span>}
      </div>
      <button className="btn btn-primary wide">Join</button>
      <button type="button" className="btn btn-ghost wide" onClick={decline}>Decline</button>
    </form>
  );
}
