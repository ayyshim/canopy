"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { draftInput, draftOf, RulesFields, useWho, WhatFields, WhoFields, type Draft } from "@/components/jobs/fields";
import { MemoryTab } from "@/components/jobs/MemoryTab";
import { ScheduleFields } from "@/components/jobs/ScheduleFields";
import { useJobEvents } from "@/components/jobs/useJobEvents";
import { PageShell } from "@/components/PageShell";
import { Card, EmptyState, TabPanel, Tabs } from "@/components/ui";
import { api, ApiError, currentOrg, isOrgAdmin, timeAgo } from "@/lib/api";
import { durationText, formatWhen, hasRunning, jobStatus, runSeconds, runStatus, tokensText, TRIGGER_LABEL, type Job, type JobRun } from "@/lib/jobs";
import { useMe } from "@/lib/me";
import type { WorkspaceSummary, WorkspaceView } from "@/lib/types";

type Detail = { workspace: WorkspaceView; viewer: { role: string } };

export function JobPage() {
  return <Suspense><Inner /></Suspense>;
}

function Inner() {
  const { me } = useMe();
  const wsId = usePathname().split("/")[2] ?? "";
  const jobId = useSearchParams().get("id") ?? "";
  const [ws, setWs] = useState<Detail | null>(null);
  const [job, setJob] = useState<Job | null>(null);
  const [runs, setRuns] = useState<JobRun[]>([]);
  const [error, setError] = useState("");
  const [gone, setGone] = useState(false);
  const [tab, setTab] = useState("runs");
  const [busy, setBusy] = useState("");
  const [version, setVersion] = useState(0);
  const ready = !!wsId && wsId !== "_" && !!jobId;

  const load = useCallback(() => {
    if (!ready) return;
    Promise.all([api<{ job: Job }>("GET", `/jobs/${jobId}`), api<{ runs: JobRun[] }>("GET", `/jobs/${jobId}/runs?limit=100`)]).then(
      ([j, r]) => { setJob(j.job); setRuns(r.runs); setVersion((v) => v + 1); },
      (e) => { if (e instanceof ApiError && e.status === 404) setGone(true); else setError(e instanceof ApiError ? e.message : "Couldn’t load the job."); },
    );
  }, [jobId, ready]);
  useEffect(load, [load]);
  useEffect(() => {
    if (ready) api<Detail>("GET", `/workspaces/${wsId}`).then(setWs, () => {});
  }, [wsId, ready]);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useJobEvents(wsId, (_t, d) => {
    if (d.id !== jobId && d.job_id !== jobId) return;
    clearTimeout(timer.current);
    timer.current = setTimeout(load, 250);
  });

  const summary: WorkspaceSummary | null = ws ? { ...ws.workspace, members: 0, directories: 0, people: 0, role: ws.viewer.role } : null;
  const admin = ws?.viewer.role === "admin" || isOrgAdmin(currentOrg(me)?.role);
  const canEdit = !!job && (admin || job.owner_user_id === me.user.id);
  const running = hasRunning(runs);

  async function act(what: "pause" | "resume" | "run" | "test") {
    setBusy(what);
    setError("");
    try {
      if (what === "run" || what === "test") await api("POST", `/jobs/${jobId}/run`, { test: what === "test" });
      else await api("POST", `/jobs/${jobId}/${what}`);
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "That didn’t work. Try again.");
    }
    setBusy("");
  }

  if (gone) return <PageShell title="Job" workspace={summary}><div className="note-box">This job isn’t here any more, or you can’t see it.</div><div><Link className="btn" href={`/w/${wsId}/jobs`}>Back to jobs</Link></div></PageShell>;
  const st = job && jobStatus(job);
  const tabs = [{ id: "runs", label: "Runs", count: runs.length }, ...(job?.kind === "continuous" ? [{ id: "memory", label: "Memory" }] : []), { id: "settings", label: "Settings" }];

  return (
    <PageShell title={job?.title ?? "Job"} workspace={summary}>
      <Link className="linkbtn" href={`/w/${wsId}/jobs`}>← All jobs</Link>
      {!job ? (error ? <div className="banner bad">{error}</div> : <p className="hint">Loading…</p>) : (
        <>
          <div className="row" style={{ flexWrap: "wrap" }}>
            <h1 className="grow" style={{ margin: 0 }}>{job.title}</h1>
            <span className="pill info">{job.kind === "single" ? "Single" : "Continuous"}</span>
            {st && <span className={`pill ${st.tone}`}>{st.label}</span>}
          </div>
          <p className="muted" style={{ margin: 0 }}>{job.schedule || job.cron}{job.status === "active" && job.next_run_at ? ` · next run ${formatWhen(job.next_run_at, job.timezone)}` : ""}</p>
          {canEdit && (
            <div className="row" style={{ flexWrap: "wrap" }}>
              <button className="btn" disabled={running || !!busy || job.status === "ended"} onClick={() => act("run")}>Run now</button>
              <button className="btn" disabled={running || !!busy || job.status === "ended"} onClick={() => act("test")} title="Same as a run, but nothing is added to memory">Test run</button>
              {job.status === "paused"
                ? <button className="btn" disabled={!!busy} onClick={() => act("resume")}>Resume</button>
                : <button className="btn" disabled={!!busy || job.status === "ended"} onClick={() => act("pause")}>Pause</button>}
            </div>
          )}
          {error && <div className="banner bad" role="alert">{error}</div>}
          <Tabs tabs={tabs} value={tab} onChange={setTab} label="Job" idBase="job" />
          <TabPanel id="runs" value={tab} idBase="job"><Runs runs={runs} wsId={wsId} zone={job.timezone} /></TabPanel>
          <TabPanel id="memory" value={tab} idBase="job"><MemoryTab job={job} runs={runs} locked={running} canEdit={canEdit} version={version} /></TabPanel>
          <TabPanel id="settings" value={tab} idBase="job"><Settings job={job} wsId={wsId} canEdit={canEdit} running={running} onSaved={load} /></TabPanel>
        </>
      )}
    </PageShell>
  );
}

function Runs({ runs, wsId, zone }: { runs: JobRun[]; wsId: string; zone: string }) {
  if (runs.length === 0) return <EmptyState icon="○">No runs yet. The first one starts at the next run time, or use Run now.</EmptyState>;
  return (
    <div className="card scroll-x">
      <table className="table stack-m">
        <thead><tr><th>Run</th><th>Due</th><th>Status</th><th>Tokens</th><th>Time taken</th><th>What happened</th></tr></thead>
        <tbody>
          {runs.map((r) => {
            const s = runStatus(r);
            return (
              <tr key={r.id}>
                <td data-l="Run"><b>#{r.number}</b>{r.trigger !== "schedule" && <> <span className="pill">{TRIGGER_LABEL[r.trigger]}</span></>}</td>
                <td data-l="Due">{formatWhen(r.scheduled_for, zone)}</td>
                <td data-l="Status"><span className={`pill ${s.tone}`}>{s.label}</span></td>
                <td data-l="Tokens">{tokensText(r.tokens)}</td>
                <td data-l="Time taken">{durationText(runSeconds(r))}</td>
                <td data-l="What happened">
                  {r.error ? <span className="err">{r.error}</span> : (r.title || r.summary) ? <>{r.title && <b>{r.title}. </b>}{r.summary}</> : <span className="muted">{r.status === "running" ? timeAgo(r.started_at) : "–"}</span>}
                  {r.goal_id && <> <Link href={`/w/${wsId}?goal=${r.goal_id}`}>Open on the board</Link></>}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function Settings({ job, wsId, canEdit, running, onSaved }: { job: Job; wsId: string; canEdit: boolean; running: boolean; onSaved: () => void }) {
  const router = useRouter();
  const who = useWho(wsId);
  const [d, setD] = useState<Draft>(() => draftOf(job));
  const [valid, setValid] = useState(true);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (p: Partial<Draft>) => { setD((x) => ({ ...x, ...p })); setMsg(null); };
  const onValid = useCallback((ok: boolean) => setValid(ok), []);

  async function save() {
    setBusy(true);
    try {
      await api("PATCH", `/jobs/${job.id}`, draftInput(d, false));
      setMsg({ ok: true, text: "Saved." });
      onSaved();
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : "Couldn’t save." });
    }
    setBusy(false);
  }

  async function remove() {
    if (!confirm(`Delete “${job.title}” and its memory? This can’t be undone.`)) return;
    try {
      await api("DELETE", `/jobs/${job.id}`);
      router.push(`/w/${wsId}/jobs`);
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError ? e.message : "Couldn’t delete." });
    }
  }

  if (!canEdit) return <div className="note-box">Only the owner of this job and workspace admins can change it.</div>;
  return (
    <div className="stack">
      <Card title="What"><WhatFields d={d} set={set} locked /></Card>
      {who && <Card title="Who"><WhoFields d={d} set={set} who={who} locked /></Card>}
      <Card title="When"><ScheduleFields wsId={wsId} value={d.schedule} zone={d.zone} onChange={(schedule) => set({ schedule })} onZone={(zone) => set({ zone })} onValid={onValid} /></Card>
      <Card title="Rules"><RulesFields d={d} set={set} /></Card>
      <div className="row" style={{ flexWrap: "wrap" }}>
        {msg && <span className={msg.ok ? "hint" : "err"} role="status">{msg.text}</span>}
        <span className="grow" />
        <button className="btn btn-primary" disabled={busy || !valid || !d.title.trim() || !d.brief.trim()} onClick={save}>Save changes</button>
      </div>
      <Card title="Delete this job" sub="Removes the job, its runs and its memory." danger>
        <button className="btn btn-danger" disabled={running} onClick={remove}>Delete job</button>
        {running && <p className="hint">A run is going. Delete the job when it finishes.</p>}
      </Card>
    </div>
  );
}
