"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { Computer } from "@/lib/types";

type Pairing = { pairing_id: string; code: string; expires_at: string; command: string };

/** CopyLine is a command with a Copy button. */
export function CopyLine({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="copybox">
      <code>{text}</code>
      <button className="btn btn-sm" type="button" onClick={async () => {
        try { await navigator.clipboard.writeText(text); setCopied(true); setTimeout(() => setCopied(false), 1500); } catch { /* select it instead */ }
      }}>{copied ? "Copied" : "Copy"}</button>
    </div>
  );
}

/** PairComputer gets a one-time code, shows the install command and waits until the computer connects. */
export function PairComputer({ orgId, onPaired, kind = "computer" }: { orgId: string; onPaired: (r: Computer) => void; kind?: "computer" | "server" }) {
  const [pairing, setPairing] = useState<Pairing | null>(null);
  const [runner, setRunner] = useState<Computer | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!pairing || runner?.online) return;
    const id = setInterval(async () => {
      const r = await api<{ paired: boolean; runner?: Computer }>("GET", `/orgs/${orgId}/runners/pairing/${pairing.pairing_id}`).catch(() => null);
      if (r?.runner) {
        setRunner(r.runner);
        if (r.runner.online) onPaired(r.runner);
      }
    }, 1500);
    return () => clearInterval(id);
  }, [pairing, runner, orgId, onPaired]);

  const start = async () => {
    setError("");
    try {
      setPairing(await api<Pairing>("POST", `/orgs/${orgId}/runners/pairing`, { kind }));
      setRunner(null);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Couldn’t get a code.");
    }
  };

  if (!pairing) {
    return (
      <div className="stack-sm">
        <div><button className="btn btn-primary" type="button" onClick={start}>Get install command</button></div>
        {error && <span className="err">{error}</span>}
      </div>
    );
  }
  const install = `curl -fsSL ${location.origin}/install.sh | sh -s -- --code ${pairing.code}`;
  return (
    <div className="card panel">
      <b>1. Install and connect</b>
      <span className="hint">On the computer, run:</span>
      <CopyLine text={install} />
      <span className="hint">It downloads canopy from this hub, pairs it with code <code>{pairing.code}</code> (valid 15 minutes) and keeps it running. Already have canopy? Run <code>{pairing.command}</code> then <code>canopy run</code>.</span>
      <b style={{ marginTop: 6 }}>2. Wait for it here</b>
      <ul className="checks" aria-live="polite">
        <li className={runner ? "ok" : ""}>{runner ? `Paired: ${runner.name}` : "Waiting for the computer to pair…"}</li>
        <li className={runner?.online ? "ok" : ""}>{runner?.online ? `Connected: ${runner.hostname}, sandbox: ${runner.sandbox || "none"}` : "Waiting for it to connect…"}</li>
      </ul>
      {runner?.online && <div className="note-box" style={{ background: "var(--green-soft)" }}><b>{runner.name}</b> is connected and the hub can reach it.</div>}
    </div>
  );
}
