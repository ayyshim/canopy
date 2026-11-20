"use client";

import { useEffect, useState } from "react";
import { Field } from "@/components/Field";
import { ProviderFields, type ProviderKey } from "@/components/ProviderFields";
import { api, ApiError, type HubSettings, type ProviderInput } from "@/lib/api";

function toInputs(s: HubSettings): Record<ProviderKey, ProviderInput> {
  return {
    github: { enabled: s.providers.github.enabled, client_id: s.providers.github.client_id, client_secret: "" },
    google: { enabled: s.providers.google.enabled, client_id: s.providers.google.client_id, client_secret: "" },
  };
}

export function HubSettingsForm() {
  const [settings, setSettings] = useState<HubSettings | null>(null);
  const [name, setName] = useState("");
  const [signupOpen, setSignupOpen] = useState(true);
  const [providers, setProviders] = useState<Record<ProviderKey, ProviderInput> | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);

  function load(s: HubSettings) {
    setSettings(s);
    setName(s.name);
    setSignupOpen(s.signup_open);
    setProviders(toInputs(s));
  }

  useEffect(() => {
    api<HubSettings>("GET", "/admin/settings").then(load);
  }, []);

  if (!settings || !providers) return <p className="sr-only" role="status">Loading…</p>;

  async function save(ev: React.FormEvent) {
    ev.preventDefault();
    setErrors({});
    setSaved(false);
    try {
      load(await api<HubSettings>("PATCH", "/admin/settings", { name, signup_open: signupOpen, providers }));
      setSaved(true);
    } catch (e) {
      setErrors(e instanceof ApiError ? (Object.keys(e.fields).length ? e.fields : { name: e.message }) : { name: "Something went wrong." });
    }
  }

  return (
    <form className="stack" onSubmit={save} noValidate>
      <h1>Hub settings</h1>
      <div className="card panel">
        <Field label="Hub name" error={errors.name}>
          {(p) => <input {...p} className="input" value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <label className="check">
          <input type="checkbox" checked={signupOpen} onChange={(e) => setSignupOpen(e.target.checked)} />
          <span>Anyone who can reach this hub may create an organization</span>
        </label>
      </div>
      <h2>Sign-in options</h2>
      <p className="muted">Email and password always work.</p>
      <ProviderFields
        value={providers}
        onChange={setProviders}
        errors={errors}
        callbackUrl={(k) => settings.providers[k].callback_url}
        hasSecret={(k) => settings.providers[k].has_secret}
      />
      <div className="row">
        <button className="btn btn-primary" type="submit">Save</button>
        {saved && <span className="muted" role="status">Saved</span>}
      </div>
    </form>
  );
}
