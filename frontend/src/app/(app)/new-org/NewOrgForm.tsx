"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { AuthShell } from "@/components/AuthShell";
import { Field, focusFirstInvalid } from "@/components/Field";
import { api, ApiError, ORG_SIZES, type OrgResponse, type OrgSize, hardNavigate } from "@/lib/api";
import { useMe } from "@/lib/me";

/** Create another organization; the current user becomes its owner and starts workspace setup. */
export function NewOrgForm() {
  const { me } = useMe();
  const [org, setOrg] = useState({ name: "", size: "" as OrgSize | "", about: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const form = useRef<HTMLFormElement>(null);

  async function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    const e: Record<string, string> = {};
    if (!org.name.trim()) e.name = "Enter your organization’s name.";
    if (!org.size) e.size = "Choose how big your organization is.";
    setErrors(e);
    if (Object.keys(e).length) return focusFirstInvalid(form.current);
    setBusy(true);
    try {
      await api<OrgResponse>("POST", "/orgs", org);
      hardNavigate("/setup");
    } catch (err) {
      setBusy(false);
      setErrors(err instanceof ApiError ? err.fields : { name: "Something went wrong. Try again." });
      focusFirstInvalid(form.current);
    }
  }

  return (
    <AuthShell>
        <form ref={form} className="authcard" onSubmit={onSubmit} noValidate>
          <div className="stack-sm">
            <h1>Create an organization</h1>
            <p className="muted">
              {me.organizations.length === 0
                ? "You’re not in an organization yet. Create one to get started."
                : "You’ll be its owner. You can switch between organizations from the top bar."}
            </p>
          </div>
          <Field label="Organization name" error={errors.name}>
            {(p) => <input {...p} className="input" autoFocus value={org.name} onChange={(e) => setOrg({ ...org, name: e.target.value })} />}
          </Field>
          <div className="field">
            <span className="label" id="new-size-label">How many people?</span>
            <div className="seg" role="radiogroup" aria-labelledby="new-size-label" aria-invalid={Boolean(errors.size)} tabIndex={-1}>
              {ORG_SIZES.map((s) => (
                <button type="button" key={s.value} role="radio" aria-checked={org.size === s.value} onClick={() => setOrg({ ...org, size: s.value })}>
                  {s.label}
                </button>
              ))}
            </div>
            {errors.size && <span className="err">{errors.size}</span>}
          </div>
          <Field label="What will agents work on? (optional)" error={errors.about}>
            {(p) => <textarea {...p} className="textarea" value={org.about} onChange={(e) => setOrg({ ...org, about: e.target.value })} />}
          </Field>
          <div className="row">
            {me.current_org_id && <Link className="btn" href="/board">Cancel</Link>}
            <span className="spacer" />
            <button className="btn btn-primary" type="submit" disabled={busy}>{busy ? "Creating…" : "Create organization"}</button>
          </div>
        </form>
    </AuthShell>
  );
}
