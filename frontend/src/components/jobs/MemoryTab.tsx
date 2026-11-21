"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Card } from "@/components/ui";
import { api, ApiError, timeAgo } from "@/lib/api";
import { runStatus, type Job, type JobMemory, type JobRun } from "@/lib/jobs";

type Revision = { rev: number; body: string; created_at: string };

/** The brief, the summary Canopy keeps between runs, its older versions and the run log. */
export function MemoryTab({ job, runs, locked, canEdit, version }: { job: Job; runs: JobRun[]; locked: boolean; canEdit: boolean; version: number }) {
  const [mem, setMem] = useState<JobMemory | null>(null);
  const [text, setText] = useState("");
  const [revs, setRevs] = useState<Revision[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const editing = useRef(false);

  const load = useCallback(async () => {
    try {
      const m = await api<JobMemory>("GET", `/jobs/${job.id}/memory`);
      setMem(m);
      if (!editing.current) setText(m.memory);
      const r = await api<{ revisions: Revision[] }>("GET", `/notes/${m.note_id}/revisions`);
      setRevs(r.revisions);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Couldn’t load the memory.");
    }
  }, [job.id]);
  // Reload when the job or a run changes; unsaved edits stay.
  useEffect(() => { load(); }, [load, version]); // eslint-disable-line react-hooks/set-state-in-effect

  const off = locked || !canEdit || busy;
  const max = mem?.max_chars ?? 5000;
  const dirty = !!mem && text !== mem.memory;

  async function put(body: string) {
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      await api("PUT", `/jobs/${job.id}/memory`, { memory: body });
      editing.current = false;
      await load();
      setSaved(true);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Couldn’t save.");
    }
    setBusy(false);
  }

  async function restore(rev: number) {
    if (!mem) return;
    setBusy(true);
    setError("");
    try {
      await api("POST", `/notes/${mem.note_id}/revisions/${rev}/restore`);
      editing.current = false;
      await load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Couldn’t restore.");
    }
    setBusy(false);
  }

  const log = runs.filter((r) => r.status !== "running").slice().reverse();

  return (
    <div className="stack">
      {error && <div className="banner bad" role="alert">{error}</div>}
      {locked && <div className="banner warn">A run is going, so memory can’t be changed until it finishes.</div>}
      <Card title="Brief" sub="Written by people. Agents never change it. Edit it in Settings.">
        <p style={{ whiteSpace: "pre-wrap", margin: 0 }}>{job.brief}</p>
      </Card>
      <Card title="Summary" sub="Rewritten by the agent after each good run. You can edit it.">
        <label className="field"><span className="lbl">What Canopy remembers</span>
          <textarea className="textarea" rows={10} value={text} disabled={off} onChange={(e) => { editing.current = true; setText(e.target.value); setSaved(false); }} maxLength={max} placeholder="Empty so far. The first successful run fills it in." /></label>
        <div className="row" style={{ marginTop: 8, flexWrap: "wrap" }}>
          <span className="hint grow">{text.length} of {max} characters{mem ? ` · updated ${timeAgo(mem.updated_at)}` : ""}{saved ? " · Saved" : ""}</span>
          <button className="btn btn-sm" disabled={off || !mem?.memory} onClick={() => { if (confirm("Clear the summary? The old text stays in the earlier versions.")) put(""); }}>Reset memory</button>
          <button className="btn btn-sm btn-primary" disabled={off || !dirty} onClick={() => put(text)}>Save</button>
        </div>
      </Card>
      <Card title="Earlier versions" sub="Restoring keeps the current text as a version, so you can undo it.">
        {revs.length === 0 ? <p className="hint">No earlier versions yet.</p> : (
          <ul className="plain-list" style={{ listStyle: "none", margin: 0, padding: 0 }}>
            {revs.map((r) => (
              <li key={r.rev} className="row" style={{ padding: "8px 0", borderTop: "1px solid var(--edge)", alignItems: "flex-start" }}>
                <div className="grow" style={{ minWidth: 0 }}>
                  <span className="hint">Version {r.rev} · {timeAgo(r.created_at)}</span>
                  <div style={{ overflow: "hidden", textOverflow: "ellipsis", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical" }}>{r.body || "(empty)"}</div>
                </div>
                <button className="btn btn-sm" disabled={off} onClick={() => restore(r.rev)}>Restore</button>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Card title="Run log" sub="One line per run. The agent sees the newest 10.">
        {log.length === 0 ? <p className="hint">No finished runs yet.</p> : (
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {log.map((r) => <li key={r.id}><b>Run {r.number}</b> · {runStatus(r).label}{r.title || r.summary ? ` · ${r.title || r.summary}` : ""}</li>)}
          </ul>
        )}
      </Card>
    </div>
  );
}
