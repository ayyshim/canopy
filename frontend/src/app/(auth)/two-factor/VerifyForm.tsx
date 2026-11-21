"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";
import { CodeInput } from "@/components/CodeInput";
import { focusFirstInvalid } from "@/components/Field";
import { logOut } from "@/components/LogoutButton";
import { api, ApiError, hardNavigate, landingPath, type Me } from "@/lib/api";
import { cleanRecoveryCode, isCodeComplete, safeNext, twoFactorPath, unauthorizedPath } from "@/lib/twoFactor";

export function VerifyForm() {
  const router = useRouter();
  const next = safeNext(useSearchParams().get("next"));
  const id = useId();
  const form = useRef<HTMLFormElement>(null);
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const [recoveryCode, setRecoveryCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    api<{ next: "enroll" | "verify" | "" }>("GET", "/auth/two-factor").then(
      async (s) => {
        if (s.next === "enroll") return router.replace(twoFactorPath("enroll", next));
        if (s.next === "") return hardNavigate(next ?? landingPath(await api<Me>("GET", "/me")));
        setReady(true);
      },
      () => router.replace("/login"),
    );
  }, [router, next]);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    setError("");
    const body = recovery ? { recovery_code: cleanRecoveryCode(recoveryCode) } : { code };
    if (recovery ? !body.recovery_code : !isCodeComplete(code)) {
      setError(recovery ? "Enter a recovery code." : "Enter the 6-digit code from your authenticator app.");
      return focusFirstInvalid(form.current);
    }
    setBusy(true);
    try {
      const me = await api<Me>("POST", "/auth/two-factor/verify", body);
      hardNavigate(next ?? landingPath(me));
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && err.code === "too_many_attempts") return hardNavigate("/login?error=too_many_codes");
      if (err instanceof ApiError && err.status === 401) return hardNavigate(unauthorizedPath(err));
      setCode("");
      setError(
        err instanceof ApiError && err.code === "invalid_code"
          ? recovery
            ? "That recovery code isn’t right, or it was already used."
            : "That code isn’t right. Wait for a new one in your app and try again."
          : err instanceof ApiError ? err.message : "Something went wrong. Try again.",
      );
      focusFirstInvalid(form.current);
    }
  }

  if (!ready) return <p className="sr-only" role="status">Loading…</p>;

  const errId = id + "-error";
  return (
    <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
      <div className="stack-sm">
        <h1>Two-step sign-in</h1>
        <p className="lead">
          {recovery
            ? "Enter one of the recovery codes you saved when you set up two-step sign-in."
            : "Open your authenticator app and enter the 6-digit code for Canopy."}
        </p>
      </div>
      {error && <div className="banner bad" role="alert" id={errId}>{error}</div>}
      <div className="field">
        <label htmlFor={id}>{recovery ? "Recovery code" : "6-digit code"}</label>
        {recovery ? (
          <input
            id={id}
            className="input mono"
            autoComplete="off"
            spellCheck={false}
            autoCapitalize="off"
            autoFocus
            aria-invalid={Boolean(error)}
            aria-describedby={error ? errId : undefined}
            value={recoveryCode}
            onChange={(e) => setRecoveryCode(e.target.value)}
          />
        ) : (
          <CodeInput id={id} value={code} onChange={setCode} invalid={Boolean(error)} describedBy={error ? errId : undefined} autoFocus />
        )}
      </div>
      <button className="btn btn-primary wide" type="submit" disabled={busy}>{busy ? "Checking…" : "Continue"}</button>
      <div className="row">
        <button
          type="button"
          className="linkbtn"
          onClick={() => {
            setRecovery(!recovery);
            setError("");
          }}
        >
          {recovery ? "Use my authenticator app instead" : "Use a recovery code instead"}
        </button>
        <span className="spacer" />
        <button type="button" className="linkbtn" onClick={() => void logOut()}>Sign out</button>
      </div>
    </form>
  );
}
