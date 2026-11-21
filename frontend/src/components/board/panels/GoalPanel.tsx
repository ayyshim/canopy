"use client";

import { RefText } from "../IdTag";
import { IdTag, CopyId } from "../IdTag";
import { useState } from "react";
import { markGoalDone, shipGoal, showGoalTasks } from "@/lib/board/actions";
import {
  clock, computer, computerFor, directory, goal as findGoal, goalRepos, goalTasks, isAdmin, memberStatus, personName,
  signInLabel, visibleMember, workTasks, firstName, plural,
} from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { GoalView } from "@/lib/types";
import { useNow } from "@/lib/useNow";
import { useB, useBoardCtx } from "../context";
import { Avatar, RepoIcon } from "../icons";
import { RunsOn, SignalBox, TaskRow, goalLabel } from "./bits";
import { Attachments } from "./Attachments";
import { DraftPanel } from "./DraftPanel";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";
import { Tabs, TabPanel, useTab, EmptyState } from "../../ui";
import { UsageBadge } from "../../ui";
import { UsageTab } from "./UsageTab";
import { HandoverBrief } from "./HandoverBrief";
import { OutputThumb } from "../OutputThumb";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function shippedText(shipped: any, base: string, branch: string): string {
  switch (shipped?.kind) {
    case "pr": return `Pull request${shipped.prs?.length > 1 ? "s" : ""} opened into ${base || "the default branch"}.`;
    case "merge": return `Merged into ${base || "the default branch"}.`;
    case "keep": return `The branch ${branch} was kept.`;
  }
  return "";
}

export function GoalPanel({ id }: { id: string }) {
  const b = useB();
  const g = findGoal(b, id);
  if (!g) return null;
  if (g.status === "draft") return <DraftPanel g={g} />;
  return <GivenGoalPanel g={g} />;
}

function GivenGoalPanel({ g }: { g: GoalView }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const [busy, setBusy] = useState("");
  const ids = ["overview", "tasks", "outputs", "activity", "usage"];
  const [tab, setTab] = useTab("goal", "overview", ids);
  const now = useNow(30000);
  const ts = goalTasks(b, g.id);
  const work = workTasks(b, g.id);
  const dn = work.filter((t) => t.status === "done").length;
  const m = visibleMember(b, g.manager_id);
  const outs = b.outputs.filter((o) => ts.some((t) => t.id === o.task_id));
  const sigs = b.signals.filter((s) => s.task_id && ts.some((t) => t.id === s.task_id));
  const byMember = [...new Set(ts.map((t) => t.member_id ?? ""))];
  const plan = outs.find((o) => o.name === "plan.md");
  const mine = g.created_by === me || isAdmin(b);
  const repos = goalRepos(b, g.id);
  const showTasks = async (goalId: string, showing: boolean) => {
    await showGoalTasks(goalId, !showing);
    const first = work[0] ?? ts[0];
    if (!showing && first) ui.centerOn(first.id);
  };
  const events = b.goal_events.filter((e) => e.goal_id === g.id).slice().reverse().slice(0, 14);
  const ends = g.finished_at ? new Date(g.finished_at).getTime() : now;
  const mins = Math.max(0, Math.round((ends - new Date(g.created_at).getTime()) / 60000));
  const base = g.base || "the default branch";
  const approved = !!g.handover_approved_by;
  const why = approved ? undefined : "The handover brief isn’t approved yet.";
  const ship = async (action: "pr" | "merge" | "keep") => {
    setBusy(action);
    await shipGoal(g.id, action);
    setBusy("");
  };

  return (
    <>
      <PanelHead tabs={<Tabs label="Goal sections" idBase="goal" value={tab} onChange={setTab} tabs={[
        { id: "overview", label: "Overview", count: sigs.length || undefined },
        { id: "tasks", label: "Tasks", count: work.length },
        { id: "outputs", label: "Outputs", count: outs.length },
        { id: "activity", label: "Activity" },
        { id: "usage", label: "Usage" },
      ]} />}>
        <div className="row" style={{ alignItems: "flex-start" }}>
          <span className="gdot" style={{ background: g.color, width: 14, height: 14, marginTop: 4 }} />
          <div style={{ minWidth: 0 }}><span className="row" style={{ gap: 6 }}><span className="tag">Goal</span><IdTag kind="goal" number={g.number} /><CopyId kind="goal" number={g.number} /></span><h2>{g.title}</h2></div>
        </div>
      </PanelHead>
      <PanelBody>
        <TabPanel id="overview" value={tab} idBase="goal"><div className="stack">
        <div className="row wrap">
          <span className={`pill ${g.status === "done" ? "ok" : g.status === "ready" ? "warn" : g.status === "planning" ? "" : "info"}`}>{goalLabel(b, g)}</span>
          <span className="hint">Given by {personName(b, g.created_by)} at {clock(g.created_at)}</span>
          <span className="spacer" /><UsageBadge usage={g.usage} />
        </div>
        <div className="box" style={{ borderColor: g.color + "55", background: g.color + "14" }}>
          <div className="row" style={{ fontSize: 12, color: "var(--muted)" }}>
            <span className="spacer">{dn} of {plural(work.length, "task")} done</span>
            <span>{outs.length} output{outs.length === 1 ? "" : "s"}, {g.status === "done" ? "took" : "running"} {mins >= 60 ? Math.floor(mins / 60) + "h " : ""}{mins % 60}m</span>
          </div>
          <div className="meter"><i style={{ width: `${work.length ? (dn / work.length) * 100 : 0}%`, background: g.color }} /></div>
        </div>

        <HandoverBrief g={g} />

        {g.status === "ready" && (
          <div className="box warn">
            <b>Every task is done</b>
            <span style={{ fontSize: 13 }}>
              {repos.length ? <>Check the outputs, then ship the code on <code>{g.branch}</code>.</> : "Check the outputs, then mark the goal as done."} Its finished tasks will be hidden from the board.
            </span>
            {approved && <span className="hint">Brief approved by {personName(b, g.handover_approved_by)} at {clock(g.handover_approved_at)}.</span>}
            {mine ? (
              <div className="stack-sm">
                {repos.length ? (
                  <>
                    <button className="btn btn-sm btn-primary" disabled={!!busy || !approved} onClick={() => ship("pr")}>{busy === "pr" ? "Opening…" : `Open pull request${repos.length > 1 ? "s" : ""} into ${base}`}</button>
                    <button className="btn btn-sm" disabled={!!busy || !approved} onClick={() => ship("merge")}>{busy === "merge" ? "Merging…" : `Merge into ${base} on ${computer(b, g.runner_id ?? computerFor(b, g.created_by ?? ""))?.name ?? "its computer"}`}</button>
                    <button className="btn btn-sm" disabled={!!busy || !approved} onClick={() => ship("keep")}>Keep the branch and mark done</button>
                  </>
                ) : (
                  <button className="btn btn-sm btn-primary" disabled={!approved} onClick={() => markGoalDone(g.id)}>Mark as done</button>
                )}
                {why && <span className="hint">{why}</span>}
                <button className="btn btn-sm" onClick={() => ui.openDialog({ kind: "task", goalId: g.id, memberId: g.manager_id ?? undefined })}>Add a follow-up task</button>
              </div>
            ) : (
              <span className="hint">Waiting for {personName(b, g.created_by)} to {repos.length ? "ship it" : "close it"}.</span>
            )}
          </div>
        )}
        {g.status === "done" && (
          <div className="box">
            <b>Done at {clock(g.finished_at)}</b>
            <span style={{ fontSize: 13 }}>
              Marked done {g.done_by ? "by " + personName(b, g.done_by) : "automatically"}. {shippedText(g.shipped, g.base, g.branch)}{" "}
              {g.show_tasks ? "Its tasks are showing on the board." : "Its tasks are hidden from the board."}
            </span>
            {g.shipped?.prs?.map((u: string) => <a key={u} href={u} target="_blank" rel="noreferrer" className="linkbtn">{u}</a>)}
            {mine && (
              <div className="row wrap">
                <button className="btn btn-sm" onClick={() => showTasks(g.id, g.show_tasks)}>{g.show_tasks ? "Hide its tasks" : "Show its tasks on the board"}</button>
                <button className="btn btn-sm" onClick={() => ui.openDialog({ kind: "task", goalId: g.id, memberId: g.manager_id ?? undefined })}>Reopen with a task</button>
              </div>
            )}
          </div>
        )}

        <RunsOn runnerId={g.runner_id} goal={g} note={`Its sessions run here with ${signInLabel(b, g.runner_id)}.`} />

        {repos.length > 0 && (
          <div className="sb-sec">
            <span className="lbl">Code</span>
            {repos.map((r) => {
              const d = directory(b, r.directory_id);
              return (
                <div className="box" key={r.directory_id}>
                  <div className="row"><RepoIcon /><code className="spacer">{d?.name ?? "repository"}</code>
                    {r.pr_url ? <a className="pill ok" href={r.pr_url} target="_blank" rel="noreferrer">Pull request</a> : r.status === "merged" ? <span className="pill ok">Merged</span> : null}</div>
                  <span className="mono" style={{ fontSize: 12 }}>{g.branch} ← {r.base || g.base || d?.default_branch || "default branch"}</span>
                  <span className="hint">
                    {r.commits} commit{r.commits === 1 ? "" : "s"} ahead · each task in its own worktree on {computer(b, g.runner_id)?.name ?? "its computer"}
                    {g.snapshot ? ` · started from ${firstName(b, g.created_by)}’s uncommitted changes` : ""}. {firstName(b, g.created_by)}’s working tree is never touched.
                  </span>
                  <span className="hint">{r.backed_up ? `Backed up to the hub after each task, so the goal can move even while ${computer(b, g.runner_id)?.name ?? "its computer"} is off.` : "Backed up to the hub after each task, so the goal can move to another computer."}</span>
                </div>
              );
            })}
          </div>
        )}

        {m && (
          <div className="sb-sec">
            <span className="lbl">Owner</span>
            <button className="trow" onClick={() => ui.openChat(m.id)}>
              <Avatar name={m.name} color={m.avatar_color} size={28} />
              <span className="spacer"><b>{m.name}</b><br /><span className="hint">Manager, {memberStatus(b, m).t}</span></span>
              <span className="pill">Chat</span>
            </button>
          </div>
        )}
        {g.description && <div className="sb-sec"><span className="lbl">Description</span><span style={{ fontSize: 13.5, whiteSpace: "pre-wrap" }}><RefText text={g.description} /></span></div>}
        {g.done_when && <div className="sb-sec"><span className="lbl">Done when</span><span style={{ fontSize: 13.5 }}><RefText text={g.done_when} /></span></div>}
        <Attachments goalId={g.id} />
        {sigs.length > 0 && <div className="sb-sec"><span className="lbl">Waiting on you ({sigs.length})</span>{sigs.map((s) => <SignalBox key={s.id} s={s} />)}</div>}
        </div></TabPanel>
        <TabPanel id="tasks" value={tab} idBase="goal"><div className="stack">
        <div className="sb-sec">
          <span className="lbl">Tasks by member</span>
          {ts.length ? byMember.map((mid) => {
            const a = visibleMember(b, mid);
            return (
              <div className="stack-sm" style={{ gap: 5 }} key={mid}>
                <div className="row" style={{ fontSize: 12.5, fontWeight: 600 }}>
                  {a ? <><Avatar name={a.name} color={a.avatar_color} size={20} />{a.name}{a.kind === "staff" && !a.reports_to && <span className="pill">helping</span>}</> : "Unassigned"}
                </div>
                {ts.filter((t) => (t.member_id ?? "") === mid).map((t) => <TaskRow key={t.id} t={t} />)}
              </div>
            );
          }) : <EmptyState icon="☐">{m?.name ?? "The manager"} is reading the goal. Tasks show up here once there is a plan.</EmptyState>}
        </div>
        </div></TabPanel>
        <TabPanel id="outputs" value={tab} idBase="goal"><div className="stack">
        {plan && (
          <div className="sb-sec">
            <span className="lbl">Plan</span>
            <span className="hint">{work.map((t, i) => `${i + 1}. ${t.title} (${visibleMember(b, t.member_id)?.name ?? ""})`).join(" · ").slice(0, 180)}…</span>
            <button className="linkbtn" style={{ alignSelf: "flex-start" }} onClick={() => ui.openViewer(plan.id)}>Open the plan</button>
          </div>
        )}
        <div className="sb-sec">
          {outs.length > 0 && <span className="lbl">Outputs</span>}
          {outs.length ? (
            <div className="thumbs">
              {outs.map((o) => (
                <button key={o.id} className={`fruit${o.status === "review" ? " review" : o.status === "approved" ? " approved" : (b.seen[o.id] ?? 0) < o.versions.length ? " new" : ""}`}
                  onClick={() => ui.openViewer(o.id)} title={o.title}><OutputThumb o={o} /></button>
              ))}
            </div>
          ) : <EmptyState icon="▢">No outputs yet. Files the agents make show up here.</EmptyState>}
        </div>
        </div></TabPanel>
        <TabPanel id="activity" value={tab} idBase="goal">
          {events.length ? (
            <ul className="timeline">
              {events.map((e, i) => <li key={i}><span className="mono hint" style={{ fontSize: 11 }}>{clock(e.at)}</span> <RefText text={e.summary} /></li>)}
            </ul>
          ) : <EmptyState icon="◷">Nothing has happened yet.</EmptyState>}
        </TabPanel>
        <TabPanel id="usage" value={tab} idBase="goal"><UsageTab usage={g.usage} tasks={ts} /></TabPanel>
      </PanelBody>
      <PanelFoot>
        <button className="btn btn-sm" onClick={() => ui.openDialog({ kind: "task", goalId: g.id })}>Add a task</button>
        <span className="spacer" />
        {m && <button className="btn btn-sm btn-primary" onClick={() => ui.openChat(m.id)}>Chat with {m.name}</button>}
      </PanelFoot>
    </>
  );
}
