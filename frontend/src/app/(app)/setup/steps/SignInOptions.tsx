"use client";

import type { StepProps } from "../Setup";

/** Advanced: how the AI helpers sign in. Defaults to the person's own Claude subscription. */
export function SignInOptions({ p, set, computer, errors }: StepProps) {
  const claude = p.clis.find((c) => c.cli === "claude")!;
  const setClaude = (patch: Partial<typeof claude>) => set((x) => ({ ...x, clis: x.clis.map((c) => (c.cli === "claude" ? { ...c, ...patch } : c)) }));
  return (
    <div className="stack-sm">
      <p className="hint">Your helpers use your Claude account by default. Only change this if you pay by usage with a key.</p>
      <div className="seg">
        <button type="button" aria-pressed={claude.mode === "subscription"} onClick={() => setClaude({ mode: "subscription" })}>My Claude account</button>
        <button type="button" aria-pressed={claude.mode === "api_key"} onClick={() => setClaude({ mode: "api_key" })}>A usage key</button>
      </div>
      {claude.mode === "api_key" && (
        <div className="row wrap" style={{ alignItems: "flex-end" }}>
          <label className="field spacer"><span>Your key</span><input className="input mono" type="password" value={claude.api_key} onChange={(e) => setClaude({ api_key: e.target.value })} placeholder="sk-ant-…" autoComplete="off" />
            {errors["clis.0.api_key"] && <span className="err">{errors["clis.0.api_key"]}</span>}</label>
          <label className="field"><span>Daily spending limit (optional)</span><input className="input tnum" inputMode="numeric" value={claude.cap_tokens_per_day ?? ""} placeholder="No limit"
            onChange={(e) => setClaude({ cap_tokens_per_day: e.target.value ? Number(e.target.value.replace(/\D/g, "")) : null })} /></label>
        </div>
      )}
      {claude.mode === "api_key" && <span className="hint">The key is saved on {computer?.name ?? "your computer"} only.</span>}
    </div>
  );
}
