"use client";

import { Field } from "./Field";
import type { ProviderInput } from "@/lib/api";

export const PROVIDERS = [
  { key: "github", label: "GitHub", help: "Create an OAuth app in GitHub → Settings → Developer settings." },
  { key: "google", label: "Google", help: "Create an OAuth client ID (web application) in Google Cloud → APIs & Services → Credentials." },
] as const;

export type ProviderKey = (typeof PROVIDERS)[number]["key"];

type Props = {
  value: Record<ProviderKey, ProviderInput>;
  onChange: (v: Record<ProviderKey, ProviderInput>) => void;
  errors: Record<string, string>;
  callbackUrl: (key: ProviderKey) => string;
  hasSecret?: (key: ProviderKey) => boolean;
};

/** Sign-in provider settings: enable, client ID, secret (write-only) and the callback URL to register. */
export function ProviderFields({ value, onChange, errors, callbackUrl, hasSecret }: Props) {
  return (
    <>
      {PROVIDERS.map((p) => {
        const v = value[p.key];
        const set = (patch: Partial<ProviderInput>) => onChange({ ...value, [p.key]: { ...v, ...patch } });
        return (
          <fieldset key={p.key} className="card panel" style={{ boxShadow: "none" }}>
            <label className="check">
              <input type="checkbox" checked={v.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
              <span>Sign in with {p.label}</span>
            </label>
            {v.enabled && (
              <>
                <p className="muted">
                  {p.help} Use this callback URL: <code>{callbackUrl(p.key)}</code>
                </p>
                <Field label={`${p.label} client ID`} error={errors[`providers.${p.key}.client_id`]}>
                  {(f) => <input {...f} className="input" value={v.client_id ?? ""} onChange={(e) => set({ client_id: e.target.value })} />}
                </Field>
                <Field
                  label={`${p.label} client secret`}
                  error={errors[`providers.${p.key}.client_secret`]}
                  hint={hasSecret?.(p.key) ? "Saved. Leave empty to keep it." : undefined}
                >
                  {(f) => <input {...f} className="input" type="password" autoComplete="off" value={v.client_secret ?? ""} onChange={(e) => set({ client_secret: e.target.value })} />}
                </Field>
              </>
            )}
          </fieldset>
        );
      })}
    </>
  );
}
