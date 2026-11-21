"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useRef, useState } from "react";
import { Field, focusFirstInvalid } from "@/components/Field";
import { api, ApiError, hardNavigate, landingPath, type Me } from "@/lib/api";
import { useHub } from "@/lib/hub";
import { pendingStep, twoFactorPath, type TwoFactorPending } from "@/lib/twoFactor";

const OAUTH_ERRORS: Record<string, string> = {
  oauth_failed: "We couldn’t sign you in with that provider. Try again.",
  oauth_unverified: "Verify your email with that provider first, then try again.",
  too_many_codes: "Too many wrong codes, so we signed you out. Sign in again to try once more.",
  oauth_no_account: "No Canopy account uses that email yet. Create an organization to get started.",
};

export function LoginForm() {
  const params = useSearchParams();
  const [email, setEmail] = useState(params.get("email") ?? "");
  const [password, setPassword] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState(OAUTH_ERRORS[params.get("error") ?? ""] ?? "");
  const hub = useHub();
  const providers = hub?.providers ?? { github: false, google: false };
  const [busy, setBusy] = useState(false);
  const form = useRef<HTMLFormElement>(null);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    const e: Record<string, string> = {};
    if (!email.trim()) e.email = "Enter your email.";
    if (!password) e.password = "Enter your password.";
    setErrors(e);
    setFormError("");
    if (Object.keys(e).length) return focusFirstInvalid(form.current);
    setBusy(true);
    try {
      const res = await api<Me | TwoFactorPending>("POST", "/auth/login", { email, password });
      const step = pendingStep(res);
      hardNavigate(step ? twoFactorPath(step) : landingPath(res as Me));
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && Object.keys(err.fields).length) {
        setErrors(err.fields);
        focusFirstInvalid(form.current);
      } else {
        setFormError(err instanceof ApiError ? err.message : "Something went wrong. Try again.");
      }
    }
  }

  return (
    <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
      <div className="stack-sm">
        <h1>Sign in</h1>
        {hub?.name && <p className="muted">to {hub.name}</p>}
      </div>
      {formError && <div className="banner bad" role="alert">{formError}</div>}
      <Field label="Email" error={errors.email}>
        {(p) => <input {...p} className="input" type="email" autoComplete="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />}
      </Field>
      <Field label="Password" error={errors.password}>
        {(p) => <input {...p} className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
      </Field>
      <div className="row">
        <Link href="/forgot">Forgot password?</Link>
        <span className="spacer" />
        <button className="btn btn-primary" type="submit" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
      </div>
      {(providers.github || providers.google) && (
        <div className="stack-sm">
          {providers.github && <a className="btn wide" href="/api/v1/auth/oauth/github/start">Continue with GitHub</a>}
          {providers.google && <a className="btn wide" href="/api/v1/auth/oauth/google/start">Continue with Google</a>}
        </div>
      )}
      {hub?.signup_open && (
        <p className="hint">
          New to Canopy? <Link href="/signup">Create an organization</Link>
        </p>
      )}
    </form>
  );
}
