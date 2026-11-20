"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { Field, focusFirstInvalid } from "@/components/Field";
import { ProviderFields, type ProviderKey } from "@/components/ProviderFields";
import { api, ApiError, hardNavigate, type Hub, type Me, type ProviderInput } from "@/lib/api";

const emptyProviders: Record<ProviderKey, ProviderInput> = {
  github: { enabled: false, client_id: "", client_secret: "" },
  google: { enabled: false, client_id: "", client_secret: "" },
};

/** First run: name the hub, choose sign-in options, create the first hub admin. */
export function HubSetupForm() {
  const params = useSearchParams();
  const router = useRouter();
  const [token, setToken] = useState(params.get("token") ?? "");
  const [hubName, setHubName] = useState("");
  const [signupOpen, setSignupOpen] = useState(true);
  const [admin, setAdmin] = useState({ name: "", email: "", password: "" });
  const [providers, setProviders] = useState(emptyProviders);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState("");
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);
  const form = useRef<HTMLFormElement>(null);

  useEffect(() => {
    api<Hub>("GET", "/hub").then((h) => (h.setup_required ? setReady(true) : router.replace("/login")), () => setReady(true));
  }, [router]);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    setFormError("");
    const e: Record<string, string> = {};
    if (!token.trim()) e.token = "Enter the setup code.";
    if (!hubName.trim()) e["hub.name"] = "Enter a name.";
    if (!admin.name.trim()) e["admin.name"] = "Enter your name.";
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(admin.email.trim())) e["admin.email"] = "Enter a valid email.";
    if (admin.password.length < 8) e["admin.password"] = "Use at least 8 characters.";
    setErrors(e);
    if (Object.keys(e).length) return focusFirstInvalid(form.current);
    setBusy(true);
    try {
      await api<Me>("POST", "/hub/setup", {
        token,
        hub: { name: hubName, signup_open: signupOpen },
        admin,
        providers: Object.fromEntries(Object.entries(providers).filter(([, p]) => p.enabled)),
      });
      hardNavigate("/admin");
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

  if (!ready) return <p className="sr-only" role="status">Loading…</p>;
  const origin = typeof window === "undefined" ? "" : window.location.origin;

  return (
    <form ref={form} className="authcard" style={{ maxWidth: 560 }} onSubmit={onSubmit} noValidate>
      <div className="stack-sm">
        <h1>Welcome to Canopy</h1>
        <p className="muted">
          Create your admin account to get started. You only do this once.
        </p>
      </div>
      {formError && <div className="banner bad" role="alert">{formError}</div>}

      <Field label="Setup code" error={errors.token} hint="Find it in the setup link that appeared when Canopy first started.">
        {(p) => <input {...p} className="input mono" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} />}
      </Field>

      <h2>Name</h2>
      <Field label="Name for this Canopy" error={errors["hub.name"]} hint="Shown on the sign-in page, such as your company name.">
        {(p) => <input {...p} className="input" value={hubName} onChange={(e) => setHubName(e.target.value)} />}
      </Field>
      <label className="check">
        <input type="checkbox" checked={signupOpen} onChange={(e) => setSignupOpen(e.target.checked)} />
        <span>Let anyone who can open this page sign up</span>
      </label>

      <h2>Other ways to sign in (optional)</h2>
      <p className="muted">Email and password always work. You can add more later.</p>
      <ProviderFields
        value={providers}
        onChange={setProviders}
        errors={errors}
        callbackUrl={(k) => `${origin}/api/v1/auth/oauth/${k}/callback`}
      />

      <h2>Your account</h2>
      <Field label="Your name" error={errors["admin.name"]}>
        {(p) => <input {...p} className="input" autoComplete="name" value={admin.name} onChange={(e) => setAdmin({ ...admin, name: e.target.value })} />}
      </Field>
      <Field label="Email" error={errors["admin.email"]}>
        {(p) => <input {...p} className="input" type="email" autoComplete="email" value={admin.email} onChange={(e) => setAdmin({ ...admin, email: e.target.value })} />}
      </Field>
      <Field label="Password" error={errors["admin.password"]} hint="At least 8 characters.">
        {(p) => <input {...p} className="input" type="password" autoComplete="new-password" value={admin.password} onChange={(e) => setAdmin({ ...admin, password: e.target.value })} />}
      </Field>
      <button className="btn btn-primary wide" type="submit" disabled={busy}>{busy ? "Setting up…" : "Get started"}</button>
    </form>
  );
}
