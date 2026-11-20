"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { Field, focusFirstInvalid } from "@/components/Field";
import { api, ApiError, hardNavigate } from "@/lib/api";
import { usePathSegment } from "@/lib/path";

export function ResetForm() {
  const token = usePathSegment("/reset/");
  const [state, setState] = useState<"checking" | "valid" | "invalid">("checking");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const form = useRef<HTMLFormElement>(null);

  useEffect(() => {
    api("GET", "/auth/reset/" + encodeURIComponent(token))
      .then(() => setState("valid"))
      .catch(() => setState("invalid"));
  }, [token]);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    if (password.length < 8) {
      setError("Use at least 8 characters.");
      return focusFirstInvalid(form.current);
    }
    setBusy(true);
    try {
      await api("POST", "/auth/reset", { token, password });
      hardNavigate("/");
    } catch (e) {
      setBusy(false);
      if (e instanceof ApiError && e.code === "invalid_token") setState("invalid");
      else setError(e instanceof ApiError ? (e.fields.password ?? e.message) : "Something went wrong. Try again.");
    }
  }

  if (state === "checking") return <p className="sr-only" role="status">Checking your link…</p>;
  if (state === "invalid") {
    return (
      <div className="authcard">
        <h1>This link no longer works</h1>
        <p className="muted">Reset links work once and expire after 30 minutes.</p>
        <Link className="btn btn-primary wide" href="/forgot">Send a new link</Link>
      </div>
    );
  }
  return (
    <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
      <h1>Choose a new password</h1>
      <Field label="New password" error={error} hint="At least 8 characters. You’ll be signed out everywhere else.">
        {(p) => <input {...p} className="input" type="password" autoComplete="new-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />}
      </Field>
      <button className="btn btn-primary wide" type="submit" disabled={busy}>{busy ? "Saving…" : "Save and sign in"}</button>
    </form>
  );
}
