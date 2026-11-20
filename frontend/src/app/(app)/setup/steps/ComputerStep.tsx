"use client";

import { useState } from "react";
import { PairComputer } from "../../(settings)/org/computers/PairComputer";
import type { StepProps } from "../Setup";
import { SignInOptions } from "./SignInOptions";

/** Connect your computer: the app on it does the work and keeps your files private. */
export function ComputerStep(props: StepProps) {
  const { computer, setComputer, set, orgId, errors } = props;
  const [where, setWhere] = useState<"computer" | "server">("computer");
  return (
    <>
      <h1>Connect your computer</h1>
      <p className="lead">Your AI team does its work on your computer, so your files never leave it. Follow the steps below to link it.</p>
      {computer?.online ? (
        <div className="note-box" style={{ background: "var(--green-soft)" }}>✓ <b>{computer.name}</b> is connected. You’re all set.</div>
      ) : (
        <>
          <button className="choice" aria-pressed={where === "computer"} onClick={() => setWhere("computer")}>
            <span style={{ textAlign: "left" }}><b>This computer</b><br /><span className="hint">The easiest way. Work pauses while it sleeps.</span></span>
          </button>
          <button className="choice" aria-pressed={where === "server"} onClick={() => setWhere("server")}>
            <span style={{ textAlign: "left" }}><b>Another computer that stays on</b><br /><span className="hint">Work keeps going when you close your laptop.</span></span>
          </button>
          <PairComputer orgId={orgId} kind={where} onPaired={(r) => { setComputer(r); set((p) => ({ ...p, runner_id: r.id })); }} />
          {computer && !computer.online && <div className="note-box" style={{ background: "var(--amber-soft)" }}><b>{computer.name}</b> is linked but switched off. Start the Canopy app on it and this page will update.</div>}
          {errors.computer && <div className="err">{errors.computer}</div>}
        </>
      )}
      <details className="instr" key={errors["clis.0.api_key"] ? "open" : "closed"} open={!!errors["clis.0.api_key"]}>
        <summary>Advanced: how your helpers sign in</summary>
        <div style={{ marginTop: 8 }}><SignInOptions {...props} /></div>
      </details>
    </>
  );
}
