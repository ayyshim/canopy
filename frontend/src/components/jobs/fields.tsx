"use client";

import { useEffect, useState } from "react";
import { api, currentOrg } from "@/lib/api";
import { browserZone, type Job, type JobInput } from "@/lib/jobs";
import { useMe } from "@/lib/me";
import { isHidden, type AnyMember, type Computer, type MemberView } from "@/lib/types";

/** Everything the job forms edit. */
export type Draft = {
  title: string; kind: "single" | "continuous"; brief: string;
  managerId: string; assigneeId: string; runnerId: string;
  schedule: string; zone: string;
  missed: "skip" | "run_once"; notify: "attention" | "always" | "failures"; capRun: string; endsOn: string;
};

export const emptyDraft = (): Draft => ({
  title: "", kind: "single", brief: "", managerId: "", assigneeId: "", runnerId: "",
  schedule: "every day at 9:00", zone: browserZone(), missed: "skip", notify: "attention", capRun: "", endsOn: "",
});

export function draftOf(j: Job): Draft {
  return {
    title: j.title, kind: j.kind, brief: j.brief, managerId: j.manager_id ?? "", assigneeId: j.assignee_member_id ?? "", runnerId: j.runner_id ?? "",
    schedule: j.cron, zone: j.timezone, missed: j.missed_policy, notify: j.notify,
    capRun: j.token_cap_per_run ? String(j.token_cap_per_run) : "", endsOn: j.ends_at ? j.ends_at.slice(0, 10) : "",
  };
}

/** The request body for a create (all fields) or an edit (without kind and manager). */
export function draftInput(d: Draft, create: boolean): JobInput {
  const cap = Number(d.capRun.replace(/[, ]/g, ""));
  const body: JobInput = {
    title: d.title.trim(), brief: d.brief.trim(), assignee_member_id: d.assigneeId || null, runner_id: d.runnerId || null,
    schedule: d.schedule.trim(), timezone: d.zone.trim(), missed_policy: d.missed,
    catchup_window_secs: d.missed === "run_once" && d.kind === "continuous" ? 6 * 3600 : 3600,
    notify: d.notify, token_caps: { per_run: cap > 0 ? cap : null },
    ends: { at: d.endsOn ? new Date(d.endsOn + "T23:59:59").toISOString() : null },
  };
  if (create) { body.kind = d.kind; body.manager_id = d.managerId; }
  return body;
}

export type Who = { managers: MemberView[]; staff: MemberView[]; computers: Computer[]; myId: string };

/** The managers, staff and computers the Who fields choose from. */
export function useWho(wsId: string): Who | null {
  const { me } = useMe();
  const orgId = currentOrg(me)?.id;
  const [members, setMembers] = useState<AnyMember[] | null>(null);
  const [computers, setComputers] = useState<Computer[]>([]);
  useEffect(() => {
    if (!wsId || wsId === "_") return;
    api<{ members: AnyMember[] }>("GET", `/workspaces/${wsId}`).then((r) => setMembers(r.members), () => setMembers([]));
  }, [wsId]);
  useEffect(() => {
    if (!orgId) return;
    api<{ runners: Computer[] }>("GET", `/orgs/${orgId}/runners`).then((r) => setComputers(r.runners), () => {});
  }, [orgId]);
  if (!members) return null;
  const real = members.filter((m): m is MemberView => !isHidden(m));
  return { managers: real.filter((m) => m.kind === "manager"), staff: real.filter((m) => m.kind === "staff"), computers, myId: me.user.id };
}

export function WhatFields({ d, set, locked }: { d: Draft; set: (p: Partial<Draft>) => void; locked?: boolean }) {
  return (
    <div className="stack">
      <label className="field"><span>Name</span>
        <input className="input" value={d.title} onChange={(e) => set({ title: e.target.value })} placeholder="Check today’s logs" maxLength={200} autoComplete="off" /></label>
      <div className="field"><span>Type</span>
        <div className="kindcards">
          {([["single", "Single run", "Each run starts fresh. Good for checks and reports."], ["continuous", "Continuous", "Each run remembers the earlier ones. Good for a series."]] as const).map(([k, name, hint]) => (
            <button key={k} type="button" className="kindcard" aria-pressed={d.kind === k} disabled={locked && d.kind !== k}
              onClick={() => !locked && set({ kind: k, missed: k === "continuous" ? "run_once" : "skip" })}>
              <b>{name}</b><span>{hint}</span>
            </button>
          ))}
        </div>
      </div>
      <label className="field"><span>Brief</span>
        <textarea className="textarea" rows={6} value={d.brief} onChange={(e) => set({ brief: e.target.value })} maxLength={20000}
          placeholder={d.kind === "continuous" ? "Describe the theme and rules. Canopy remembers what each run did." : "What should the agent do each time?"} /></label>
      {d.kind === "continuous" && <p className="hint">Canopy keeps a short summary after every run and shows the agent the last few runs.</p>}
    </div>
  );
}

export function WhoFields({ d, set, who, locked }: { d: Draft; set: (p: Partial<Draft>) => void; who: Who; locked?: boolean }) {
  const mine = who.computers.filter((c) => c.shared || c.owner_user_id === who.myId);
  const team = who.staff.filter((s) => s.reports_to === d.managerId);
  return (
    <div className="stack">
      <label className="field"><span>Manager</span>
        <select className="input" value={d.managerId} disabled={locked} onChange={(e) => set({ managerId: e.target.value, assigneeId: "" })}>
          <option value="">Choose a manager</option>
          {who.managers.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select></label>
      <label className="field"><span>Who does the work</span>
        <select className="input" value={d.assigneeId} onChange={(e) => set({ assigneeId: e.target.value })}>
          <option value="">The manager</option>
          {team.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select></label>
      <label className="field"><span>Computer</span>
        <select className="input" value={d.runnerId} onChange={(e) => set({ runnerId: e.target.value })}>
          <option value="">My computer</option>
          {mine.map((c) => <option key={c.id} value={c.id}>{c.name}{c.shared ? " (shared)" : ""}</option>)}
        </select></label>
      <p className="hint">The job runs on this computer. If it is off at run time, the run is skipped. A shared computer keeps jobs running when laptops are off.</p>
    </div>
  );
}

export function RulesFields({ d, set }: { d: Draft; set: (p: Partial<Draft>) => void }) {
  return (
    <div className="stack">
      <label className="field"><span>If Canopy was down at run time</span>
        <select className="input" value={d.missed} onChange={(e) => set({ missed: e.target.value as Draft["missed"] })}>
          <option value="skip">Skip that run</option>
          <option value="run_once">Run once when it is back{d.kind === "continuous" ? " (within 6 hours)" : " (within 1 hour)"}</option>
        </select></label>
      <label className="field"><span>Tell me</span>
        <select className="input" value={d.notify} onChange={(e) => set({ notify: e.target.value as Draft["notify"] })}>
          <option value="attention">When something needs a look, or a run fails</option>
          <option value="always">After every run</option>
          <option value="failures">Only when a run fails</option>
        </select></label>
      <label className="field"><span>Token limit per run (optional)</span>
        <input className="input" inputMode="numeric" value={d.capRun} onChange={(e) => set({ capRun: e.target.value.replace(/[^\d,]/g, "") })} placeholder="No limit" />
        <span className="hint">A run that goes over is stopped.</span></label>
      <label className="field"><span>Stop after (optional)</span>
        <input className="input" type="date" value={d.endsOn} onChange={(e) => set({ endsOn: e.target.value })} /></label>
      <p className="hint">After 3 failed runs in a row the job pauses itself.</p>
    </div>
  );
}
