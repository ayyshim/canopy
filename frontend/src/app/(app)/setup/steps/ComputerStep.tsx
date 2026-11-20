"use client";

import { useState } from "react";
import { PairComputer } from "../../(settings)/org/computers/PairComputer";
import type { StepProps } from "../Setup";

/** Connect your computer: the client lists and syncs directories and runs your goals. */
export function ComputerStep({ computer, setComputer, set, orgId, errors }: StepProps) {
  const [where, setWhere] = useState<"computer" | "server">("computer");
  return (
    <>
      <h1>Connect your computer</h1>
      <p className="lead">The <code>canopy</code> client links this computer to the hub. Goals you give run on it with your own CLI sign-in, in a sandbox that sees only the folders they’re given, and it’s how you share repositories and folders. It connects out to the hub, so no ports need opening.</p>
      {computer?.online ? (
        <div className="note-box" style={{ background: "var(--green-soft)" }}><b>{computer.name}</b> is connected and the hub can reach it ({computer.hostname}, sandbox: {computer.sandbox || "none"}).</div>
      ) : (
        <>
          <button className="choice" aria-pressed={where === "computer"} onClick={() => setWhere("computer")}>
            <span style={{ textAlign: "left" }}><b>This computer</b><br /><span className="hint">Quickest to set up. When it sleeps, your goals pause and resume when it wakes.</span></span>
          </button>
          <button className="choice" aria-pressed={where === "server"} onClick={() => setWhere("server")}>
            <span style={{ textAlign: "left" }}><b>A remote server</b><br /><span className="hint">Your goals keep running with your laptop closed. Run the same command there.</span></span>
          </button>
          <PairComputer orgId={orgId} kind={where} onPaired={(r) => { setComputer(r); set((p) => ({ ...p, runner_id: r.id })); }} />
          {computer && !computer.online && <div className="note-box" style={{ background: "var(--amber-soft)" }}><b>{computer.name}</b> is paired but not connected. Start it with <code>canopy run</code>.</div>}
          {errors.computer && <div className="err">{errors.computer}</div>}
        </>
      )}
    </>
  );
}
