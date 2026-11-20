"use client";

import type { StepProps } from "../Setup";

const LOGO: Record<string, [string, string]> = { claude: ["CC", "#D97757"], codex: ["CX", "#14181F"], gemini: ["GE", "#2E5FB8"], pi: ["PI", "#8E5BD9"] };

/** Connect your CLIs: how each CLI signs in on your computer. Your goals use this sign-in. */
export function Clis({ p, set, clis, computer, errors }: StepProps) {
  const detected = (computer?.clis ?? []) as { cli: string; installed: boolean; version: string; logged_in: boolean }[];
  const claude = p.clis.find((c) => c.cli === "claude")!;
  const setClaude = (patch: Partial<typeof claude>) => set((x) => ({ ...x, clis: x.clis.map((c) => (c.cli === "claude" ? { ...c, ...patch } : c)) }));
  return (
    <>
      <h1>Connect your agent CLIs</h1>
      <p className="lead">Choose how each CLI signs in on {computer?.name ?? "your computer"}. Your goals use this sign-in; teammates’ goals use theirs. Subscriptions have usage limits that reset over time. API keys are billed per use, so you can set a cap.</p>
      {clis.map((info) => {
        const d = detected.find((x) => x.cli === info.id);
        const [ini, color] = LOGO[info.id] ?? [info.name.slice(0, 2).toUpperCase(), "#4A5462"];
        const status = d?.installed ? `Installed${d.version ? ", " + d.version : ""}${d.logged_in ? ", signed in" : ""}${info.supported ? "" : ". Coming soon to Canopy"}` : info.supported ? "Not found on this computer" : "Not installed. Coming soon to Canopy";
        return (
          <div className="cli" key={info.id}>
            <div className="cli-top">
              <span className="cli-logo" style={{ background: color }}>{ini}</span>
              <span className="spacer"><b>{info.name}</b><br /><span className="hint">{status}</span></span>
              {info.supported ? <label className="toggle"><input type="checkbox" checked readOnly /> Use</label>
                : !d?.installed && info.install_cmd ? <button className="btn btn-sm" type="button" onClick={() => navigator.clipboard?.writeText(info.install_cmd).catch(() => {})}>Copy install command</button>
                : <label className="toggle"><input type="checkbox" disabled /> Use</label>}
            </div>
            {info.id === "claude" && (
              <div className="stack-sm" style={{ marginLeft: 54 }}>
                <div className="seg">
                  <button type="button" aria-pressed={claude.mode === "subscription"} onClick={() => setClaude({ mode: "subscription" })}>Claude subscription</button>
                  <button type="button" aria-pressed={claude.mode === "api_key"} onClick={() => setClaude({ mode: "api_key" })}>Anthropic API key</button>
                </div>
                {claude.mode === "api_key" ? (
                  <div className="row wrap" style={{ alignItems: "flex-end" }}>
                    <label className="field spacer"><span>API key</span><input className="input mono" type="password" value={claude.api_key} onChange={(e) => setClaude({ api_key: e.target.value })} placeholder="sk-ant-…" autoComplete="off" />
                      {errors["clis.0.api_key"] && <span className="err">{errors["clis.0.api_key"]}</span>}</label>
                    <label className="field"><span>Daily cap (tokens)</span><input className="input tnum" inputMode="numeric" value={claude.cap_tokens_per_day ?? ""} placeholder="No cap"
                      onChange={(e) => setClaude({ cap_tokens_per_day: e.target.value ? Number(e.target.value.replace(/\D/g, "")) : null })} /></label>
                  </div>
                ) : <span className="hint">Canopy can’t see how much usage is left, so it tells you when Claude Code reports a limit and when it resets.</span>}
                {claude.mode === "api_key" && <span className="hint">The key goes straight to Claude Code’s config on {computer?.name}. The hub keeps only its last four characters.</span>}
              </div>
            )}
          </div>
        );
      })}
    </>
  );
}
