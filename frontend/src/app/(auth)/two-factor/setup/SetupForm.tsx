"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";
import { CodeInput } from "@/components/CodeInput";
import { CopyButton } from "@/components/CopyButton";
import { RecoveryCodes } from "@/components/RecoveryCodes";
import { focusFirstInvalid } from "@/components/Field";
import { logOut } from "@/components/LogoutButton";
import { api, ApiError, hardNavigate, landingPath, type Me } from "@/lib/api";
import {
  isCodeComplete,
  safeNext,
  twoFactorPath,
  type TwoFactorEnabled,
  type TwoFactorSetup,
} from "@/lib/twoFactor";

export function SetupForm() {
  const router = useRouter();
  const next = safeNext(useSearchParams().get("next"));
  const id = useId();
  const form = useRef<HTMLFormElement>(null);
  const started = useRef(false);
  const [setup, setSetup] = useState<TwoFactorSetup | null>(null);
  const [failed, setFailed] = useState(false);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<{ codes: string[]; me: Me } | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    // Each call makes a new secret, so only ask once.
    if (started.current) return;
    started.current = true;
    (async () => {
      const s = await api<{ next: "enroll" | "verify" | "" }>("GET", "/auth/two-factor");
      if (s.next === "verify") return router.replace(twoFactorPath("verify", next));
      if (s.next === "") return hardNavigate(next ?? landingPath(await api<Me>("GET", "/me")));
      setSetup(await api<TwoFactorSetup>("POST", "/auth/two-factor/setup"));
    })().catch((err) => {
      if (err instanceof ApiError && err.status === 401) router.replace("/login");
      else setFailed(true);
    });
  }, [router, next]);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    setError("");
    if (!isCodeComplete(code)) {
      setError("Enter the 6-digit code from your authenticator app.");
      return focusFirstInvalid(form.current);
    }
    setBusy(true);
    try {
      const r = await api<TwoFactorEnabled>("POST", "/auth/two-factor/enable", { code });
      setDone({ codes: r.recovery_codes, me: r.me });
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && err.code === "too_many_attempts") return hardNavigate("/login?error=too_many_codes");
      setCode("");
      setError(err instanceof ApiError ? err.message : "Something went wrong. Try again.");
      focusFirstInvalid(form.current);
    }
  }

  if (failed) {
    return (
      <div className="authcard">
        <h1>We couldn’t start the setup</h1>
        <p className="lead">Check your connection and try again.</p>
        <button className="btn btn-primary wide" onClick={() => location.reload()}>Try again</button>
      </div>
    );
  }
  if (done) {
    return (
      <div className="authcard">
        <div className="stack-sm">
          <h1>Save your recovery codes</h1>
          <p className="lead">
            If you lose your phone, each of these codes lets you sign in once. We show them only now, so keep them somewhere safe.
          </p>
        </div>
        <RecoveryCodes codes={done.codes} />
        <label className="check-row">
          <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
          <span>I saved these codes</span>
        </label>
        <button className="btn btn-primary wide" disabled={!saved} onClick={() => hardNavigate(next ?? landingPath(done.me))}>
          Continue
        </button>
      </div>
    );
  }
  if (!setup) return <p className="sr-only" role="status">Loading…</p>;

  const errId = id + "-error";
  return (
    <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
      <div className="stack-sm">
        <h1>Set up two-step sign-in</h1>
        <p className="lead">Everyone on Canopy signs in with a code from an authenticator app, as well as a password.</p>
      </div>
      <ol className="tf-steps">
        <li>
          <b>Scan this code</b> with an authenticator app such as 1Password, Google Authenticator or Authy.
          {/* The QR code is a data: URL made by the hub, so next/image adds nothing. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img className="qr" src={setup.qr_png} width={200} height={200} alt="QR code for your authenticator app" />
          <span className="hint" style={{ display: "block" }}>Can’t scan it? Enter this key in your app instead.</span>
          <div className="keybox">
            <code className="mono" aria-label="Setup key">{setup.secret}</code>
            <CopyButton text={setup.secret} />
          </div>
        </li>
        <li>
          <b>Enter the 6-digit code</b> your app shows.
        </li>
      </ol>
      {error && <div className="banner bad" role="alert" id={errId}>{error}</div>}
      <div className="field">
        <label htmlFor={id}>6-digit code</label>
        <CodeInput id={id} value={code} onChange={setCode} invalid={Boolean(error)} describedBy={error ? errId : undefined} />
      </div>
      <button className="btn btn-primary wide" type="submit" disabled={busy}>{busy ? "Checking…" : "Turn on two-step sign-in"}</button>
      <button type="button" className="linkbtn" onClick={() => void logOut()}>Sign out</button>
    </form>
  );
}
