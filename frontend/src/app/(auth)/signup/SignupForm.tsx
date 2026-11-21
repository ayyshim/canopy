"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { Field, focusFirstInvalid } from "@/components/Field";
import { api, ApiError, hardNavigate, ORG_SIZES, type Me, type OrgSize } from "@/lib/api";
import { useHub } from "@/lib/hub";
import { pendingStep, twoFactorPath, type TwoFactorPending } from "@/lib/twoFactor";

type Errors = Record<string, string>;

/** Two-step sign up: the organization first, then you (SPEC "Sign up"). */
export function SignupForm() {
  const hub = useHub();
  const [step, setStep] = useState<1 | 2>(1);
  const [org, setOrg] = useState({ name: "", size: "" as OrgSize | "", about: "" });
  const [user, setUser] = useState({ name: "", email: "", password: "", accept_terms: false });
  const [errors, setErrors] = useState<Errors>({});
  const [emailTaken, setEmailTaken] = useState(false);
  const [formError, setFormError] = useState("");
  const [busy, setBusy] = useState(false);
  const form = useRef<HTMLFormElement>(null);

  function show(errs: Errors) {
    setErrors(errs);
    focusFirstInvalid(form.current);
    return Object.keys(errs).length === 0;
  }

  function validateOrg(): boolean {
    const e: Errors = {};
    if (!org.name.trim()) e["org.name"] = "Enter your company or team name.";
    if (!org.size) e["org.size"] = "Choose how many people.";
    return show(e);
  }

  function validateUser(): boolean {
    const e: Errors = {};
    if (!user.name.trim()) e["user.name"] = "Enter your name.";
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(user.email.trim())) e["user.email"] = "Enter a valid email address.";
    if (user.password.length < 8) e["user.password"] = "Use at least 8 characters.";
    if (!user.accept_terms) e["user.accept_terms"] = "Please tick the box to continue.";
    return show(e);
  }

  async function submit() {
    setBusy(true);
    setFormError("");
    setEmailTaken(false);
    try {
      const res = await api<Me | TwoFactorPending>("POST", "/auth/signup", { org, user });
      const step = pendingStep(res);
      hardNavigate(step ? twoFactorPath(step, "/setup") : "/setup");
    } catch (e) {
      setBusy(false);
      if (!(e instanceof ApiError)) return setFormError("Something went wrong. Try again.");
      if (e.code === "email_taken") setEmailTaken(true);
      if (Object.keys(e.fields).some((f) => f.startsWith("org."))) setStep(1);
      if (Object.keys(e.fields).length) show(e.fields);
      else setFormError(e.message);
    }
  }

  function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    if (step === 1) {
      if (validateOrg()) {
        setStep(2);
        setErrors({});
      }
    } else if (validateUser()) {
      void submit();
    }
  }

  if (hub && !hub.signup_open) {
    return (
      <div className="authcard">
        <h1>Sign-ups are closed</h1>
        <p className="muted">{hub.name} isn’t accepting new sign-ups. Ask the person who runs it to invite you.</p>
        <Link className="btn wide" href="/login">Sign in</Link>
      </div>
    );
  }

  return (
    <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
      <div className="stack-sm">
        <span className="muted">Step {step} of 2</span>
        <h1>{step === 1 ? "Create your account" : "About you"}</h1>
      </div>
      {formError && <div className="banner bad" role="alert">{formError}</div>}

      {step === 1 ? (
        <>
          <Field label="Company or team name" error={errors["org.name"]}>
            {(p) => (
              <input {...p} className="input" autoFocus autoComplete="organization" value={org.name}
                onChange={(e) => setOrg({ ...org, name: e.target.value })} />
            )}
          </Field>
          <div className="field">
            <span className="label" id="size-label">How many people will use it?</span>
            <div className="seg" role="radiogroup" aria-labelledby="size-label" aria-invalid={Boolean(errors["org.size"])} tabIndex={-1}>
              {ORG_SIZES.map((s) => (
                <button type="button" key={s.value} role="radio" aria-checked={org.size === s.value}
                  onClick={() => setOrg({ ...org, size: s.value })}>
                  {s.label}
                </button>
              ))}
            </div>
            {errors["org.size"] && <span className="err">{errors["org.size"]}</span>}
          </div>
          <Field label="What will your AI team help with? (optional)" error={errors["org.about"]}>
            {(p) => (
              <textarea {...p} className="textarea" placeholder="For example: building our website" value={org.about}
                onChange={(e) => setOrg({ ...org, about: e.target.value })} />
            )}
          </Field>
          <button className="btn btn-primary wide" type="submit">Continue</button>
        </>
      ) : (
        <>
          <Field label="Your name" error={errors["user.name"]}>
            {(p) => (
              <input {...p} className="input" autoFocus autoComplete="name" value={user.name}
                onChange={(e) => setUser({ ...user, name: e.target.value })} />
            )}
          </Field>
          <Field
            label="Email"
            error={errors["user.email"]}
            hint={emailTaken ? (
              <>
                <Link href={`/login?email=${encodeURIComponent(user.email.trim())}`}>Sign in</Link> to continue.
              </>
            ) : undefined}
          >
            {(p) => (
              <input {...p} className="input" type="email" autoComplete="email" value={user.email}
                onChange={(e) => setUser({ ...user, email: e.target.value })} />
            )}
          </Field>
          <Field label="Password" error={errors["user.password"]} hint="At least 8 characters.">
            {(p) => (
              <input {...p} className="input" type="password" autoComplete="new-password" value={user.password}
                onChange={(e) => setUser({ ...user, password: e.target.value })} />
            )}
          </Field>
          <div className="field">
            <label className="check">
              <input type="checkbox" checked={user.accept_terms} aria-invalid={Boolean(errors["user.accept_terms"])}
                onChange={(e) => setUser({ ...user, accept_terms: e.target.checked })} />
              <span>I agree to the terms of service and privacy policy.</span>
            </label>
            {errors["user.accept_terms"] && <span className="err">{errors["user.accept_terms"]}</span>}
          </div>
          <div className="row">
            <button className="btn" type="button" onClick={() => { setStep(1); setErrors({}); }}>Back</button>
            <span className="spacer" />
            <button className="btn btn-primary" type="submit" disabled={busy}>{busy ? "Creating…" : "Create account"}</button>
          </div>
        </>
      )}
      <p className="hint">
        Already have an account? <Link href="/login">Sign in</Link>
      </p>
    </form>
  );
}
