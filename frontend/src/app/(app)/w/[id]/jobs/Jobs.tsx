"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { CreateJobDialog } from "@/components/jobs/CreateJobDialog";
import { useJobEvents } from "@/components/jobs/useJobEvents";
import { PageShell } from "@/components/PageShell";
import { EmptyState } from "@/components/ui";
import { api, ApiError, currentOrg, isOrgAdmin, timeAgo } from "@/lib/api";
import { formatWhen, jobStatus, runStatus, type Job } from "@/lib/jobs";
import { useMe } from "@/lib/me";
import type { WorkspaceSummary, WorkspaceView } from "@/lib/types";

type Detail = { workspace: WorkspaceView; viewer: { role: string } };

export function Jobs() {
  return <Suspense><Inner /></Suspense>;
}

function Inner() {
  const { me } = useMe();
  const router = useRouter();
  const wsId = usePathname().split("/")[2] ?? "";
  const [ws, setWs] = useState<Detail | null>(null);
  const [jobs, setJobs] = useState<Job[] | null>(null);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState("");
  const ready = !!wsId && wsId !== "_";

  const load = useCallback(() => {
    if (!ready) return;
    api<{ jobs: Job[] }>("GET", `/workspaces/${wsId}/jobs`).then((r) => setJobs(r.jobs), (e) => setError(e instanceof ApiError ? e.message : "Couldn’t load jobs."));
  }, [wsId, ready]);
  useEffect(load, [load]);
  useEffect(() => {
    if (!ready) return;
    api<Detail>("GET", `/workspaces/${wsId}`).then(setWs, () => {});
  }, [wsId, ready]);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useJobEvents(wsId, () => { clearTimeout(timer.current); timer.current = setTimeout(load, 250); });

  const summary: WorkspaceSummary | null = ws ? { ...ws.workspace, members: 0, directories: 0, people: 0, role: ws.viewer.role } : null;
  const admin = ws?.viewer.role === "admin" || isOrgAdmin(currentOrg(me)?.role);
  const can = (j: Job) => admin || j.owner_user_id === me.user.id;

  async function act(j: Job, what: "run" | "pause" | "resume") {
    setBusy(j.id + what);
    setError("");
    try {
      await api("POST", `/jobs/${j.id}/${what}`, what === "run" ? {} : undefined);
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "That didn’t work. Try again.");
    }
    setBusy("");
  }

  return (
    <PageShell title="Jobs" workspace={summary}>
      <div className="row" style={{ flexWrap: "wrap" }}>
        <h1 className="grow" style={{ margin: 0 }}>Jobs</h1>
        <button className="btn btn-primary" onClick={() => setCreating(true)} disabled={!ready}>+ Create a job</button>
      </div>
      <p className="hint">A job gives a manager’s team a task on a schedule. Each run shows up on the board as a goal.</p>
      {error && <div className="banner bad" role="alert">{error}</div>}
      {!jobs ? <p className="hint">Loading…</p> : jobs.length === 0 ? (
        <EmptyState icon="⏱" action={<button className="btn btn-primary" onClick={() => setCreating(true)}>Create a job</button>}>
          No jobs yet. Create one to have work start on its own, like a daily check or a weekly report.
        </EmptyState>
      ) : (
        <div className="card scroll-x">
          <table className="table stack-m">
            <thead><tr><th>Job</th><th>When</th><th>Next run</th><th>Last run</th><th>Status</th><th /></tr></thead>
            <tbody>
              {jobs.map((j) => {
                const st = jobStatus(j);
                const last = j.last_run ? runStatus(j.last_run) : null;
                const running = j.last_run?.status === "running";
                return (
                  <tr key={j.id}>
                    <td data-l="Job">
                      <Link href={`/w/${wsId}/jobs/job?id=${j.id}`}><b>{j.title}</b></Link>{" "}
                      <span className="pill info">{j.kind === "single" ? "Single" : "Continuous"}</span>
                    </td>
                    <td data-l="When">{j.schedule || j.cron}</td>
                    <td data-l="Next run">{j.status === "active" ? formatWhen(j.next_run_at, j.timezone) : "–"}</td>
                    <td data-l="Last run">{last && j.last_run ? <><span className={`pill ${last.tone}`}>{last.label}</span> <span className="hint">{timeAgo(j.last_run.started_at ?? j.last_run.scheduled_for)}</span></> : <span className="muted">Not yet</span>}</td>
                    <td data-l="Status"><span className={`pill ${st.tone}`}>{st.label}</span></td>
                    <td>
                      {can(j) && (
                        <div className="row" style={{ flexWrap: "wrap" }}>
                          <button className="btn btn-sm" disabled={running || !!busy || j.status === "ended"} onClick={() => act(j, "run")}>Run now</button>
                          {j.status === "paused"
                            ? <button className="btn btn-sm" disabled={!!busy} onClick={() => act(j, "resume")}>Resume</button>
                            : <button className="btn btn-sm" disabled={!!busy || j.status === "ended"} onClick={() => act(j, "pause")}>Pause</button>}
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {creating && <CreateJobDialog wsId={wsId} onClose={() => setCreating(false)} onCreated={(j) => router.push(`/w/${wsId}/jobs/job?id=${j.id}`)} />}
    </PageShell>
  );
}
