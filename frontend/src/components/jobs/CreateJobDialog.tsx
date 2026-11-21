"use client";

import { useCallback, useState } from "react";
import { Modal } from "@/components/Modal";
import { api, ApiError } from "@/lib/api";
import type { Job } from "@/lib/jobs";
import { draftInput, emptyDraft, RulesFields, useWho, WhatFields, WhoFields, type Draft } from "./fields";
import { ScheduleFields } from "./ScheduleFields";

const STEPS = ["What", "Who", "When", "Rules"];

/** Four steps: what to do, who does it, when, and the rules. */
export function CreateJobDialog({ wsId, onClose, onCreated }: { wsId: string; onClose: () => void; onCreated: (j: Job) => void }) {
  const who = useWho(wsId);
  const [d, setD] = useState<Draft>(emptyDraft);
  const [step, setStep] = useState(0);
  const [valid, setValid] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = (p: Partial<Draft>) => setD((x) => ({ ...x, ...p }));
  const onValid = useCallback((ok: boolean) => setValid(ok), []);
  const manager = d.managerId || (who?.managers.length === 1 ? who.managers[0].id : "");
  const draft = { ...d, managerId: manager };

  const missing = [
    !d.title.trim() || !d.brief.trim() ? "Add a name and a brief." : "",
    !manager ? "Choose a manager." : "",
    !valid ? "Pick a schedule that works." : "",
    "",
  ][step];

  async function create() {
    setBusy(true);
    setError("");
    try {
      const r = await api<{ job: Job }>("POST", `/workspaces/${wsId}/jobs`, draftInput(draft, true));
      onCreated(r.job);
    } catch (e) {
      setError(e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : "Couldn’t create the job.");
      setBusy(false);
    }
  }

  return (
    <Modal title="Create a job" onClose={onClose}>
      <ol className="steps" aria-label="Steps" style={{ display: "flex", gap: 6, listStyle: "none", margin: 0, padding: 0, flexWrap: "wrap" }}>
        {STEPS.map((s, i) => (
          <li key={s}><button type="button" className="pill" aria-current={i === step ? "step" : undefined}
            style={i === step ? { background: "var(--accent-soft)", color: "var(--ink)" } : undefined}
            onClick={() => i < step && setStep(i)} disabled={i > step}>{i + 1}. {s}</button></li>
        ))}
      </ol>
      {!who ? <p className="hint">Loading…</p> : (
        <>
          {step === 0 && <WhatFields d={d} set={set} />}
          {step === 1 && <WhoFields d={draft} set={set} who={who} />}
          {step === 2 && <ScheduleFields wsId={wsId} value={d.schedule} zone={d.zone} onChange={(schedule) => set({ schedule })} onZone={(zone) => set({ zone })} onValid={onValid} />}
          {step === 3 && <RulesFields d={d} set={set} />}
        </>
      )}
      {error && <div className="banner bad" role="alert">{error}</div>}
      <div className="row" style={{ justifyContent: "flex-end", flexWrap: "wrap" }}>
        {missing && step < 3 && <span className="hint grow">{missing}</span>}
        <button type="button" className="btn" onClick={step === 0 ? onClose : () => setStep(step - 1)} disabled={busy}>{step === 0 ? "Cancel" : "Back"}</button>
        {step < 3
          ? <button type="button" className="btn btn-primary" disabled={!!missing || !who} onClick={() => setStep(step + 1)}>Next</button>
          : <button type="button" className="btn btn-primary" disabled={busy} onClick={create}>{busy ? "Creating…" : "Create job"}</button>}
      </div>
    </Modal>
  );
}
