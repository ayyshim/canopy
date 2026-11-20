"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { Field, focusFirstInvalid } from "@/components/Field";
import { api, ApiError } from "@/lib/api";

export function ForgotForm() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const form = useRef<HTMLFormElement>(null);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    setBusy(true);
    try {
      await api("POST", "/auth/forgot", { email });
      router.push("/sent?email=" + encodeURIComponent(email.trim()));
    } catch (e) {
      setBusy(false);
      setError(e instanceof ApiError ? (e.fields.email ?? e.message) : "Something went wrong. Try again.");
      focusFirstInvalid(form.current);
    }
  }

  return (
    <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
      <div className="stack-sm">
        <h1>Reset your password</h1>
        <p className="muted">We’ll email you a link to choose a new password.</p>
      </div>
      <Field label="Email" error={error}>
        {(p) => <input {...p} className="input" type="email" autoComplete="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />}
      </Field>
      <button className="btn btn-primary wide" type="submit" disabled={busy}>{busy ? "Sending…" : "Send reset link"}</button>
      <p className="hint"><Link href="/login">Back to sign in</Link></p>
    </form>
  );
}
