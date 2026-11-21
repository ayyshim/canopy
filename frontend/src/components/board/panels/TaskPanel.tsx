"use client";

import { useCatalog, skillLabel } from "@/lib/catalog";
import { useEffect, useState } from "react";
import { loadActivity, loadTask, postComment } from "@/lib/board/actions";
import {
  ago, clock, directory, goal as findGoal, memberStatus, outputsOf, personName, runnerForTask, signInLabel, signalsForTask, task as findTask,
  visibleMember,
} from "@/lib/board/derive";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { useB, useBoardCtx } from "../context";
import { Avatar } from "../icons";
import { RunsOn, SignalBox, StatusPill, TaskRow, shortTitle } from "./bits";
import { Attachments } from "./Attachments";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";
import { OutputThumb } from "../OutputThumb";

export function TaskPanel({ id }: { id: string }) {
  useCatalog();
  const b = useB();
  const { wsId } = useBoardCtx();
  const ui = useUI();
  const t = findTask(b, id)!;
  const m = visibleMember(b, t.member_id);
  const g = findGoal(b, t.goal_id);
  const comments = useBoard((s) => s.comments[id]);
  const logs = useBoard((s) => (t.member_id ? s.logs[t.member_id] : undefined));
  const [text, setText] = useState("");

  useEffect(() => {
    loadTask(id);
    if (t.member_id) loadActivity(wsId, t.member_id);
  }, [id, wsId, t.member_id]);

  const sigs = signalsForTask(b, t.id);
  const outs = outputsOf(b, t.id);
  const unlocks = b.tasks.filter((x) => x.depends_on.includes(t.id));
  const waits = t.depends_on.filter((d) => findTask(b, d)?.status !== "done").length;
  const runner = runnerForTask(b, t);
  const taskLogs = (logs ?? []).filter((l) => l.task_id === t.id).slice(-10).reverse();
  const repoBranch = (dirId: string) => {
    const d = directory(b, dirId);
    if (!d) return "";
    if (d.is_repo) return `${d.name}: a worktree on ${g?.branch ?? "its own branch"}`;
    return `${d.name}: ${d.machine_id === runner ? "in place, on the owner’s computer" : `the hub copy; changes go to ${d.owner_name}`}`;
  };
  const reach = b.grants.filter((x) => x.member_id === t.member_id).map((x) => `${directory(b, x.directory_id)?.name} (${x.mode === "read_write" ? "read & write" : "read"})`).join(", ");
  const steps = t.steps ?? [];
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!text.trim()) return;
    if (await postComment(t.id, text.trim())) {
      setText("");
      loadTask(t.id);
    }
  };

  return (
    <>
      <PanelHead><span className="tag">Task</span><h2>{t.title}</h2></PanelHead>
      <PanelBody>
        <div className="row wrap">
          <StatusPill t={t} />
          {g && (
            <button className="pill" style={{ cursor: "pointer" }} onClick={() => ui.openSide({ type: "goal", id: g.id })}>
              <span className="gdot" style={{ background: g.color, width: 8, height: 8 }} />{shortTitle(g.title)}
            </button>
          )}
          <button className="linkbtn" onClick={() => { ui.centerOn(t.id); ui.setGlow(t.id); }}>Show on the tree</button>
        </div>
        {t.kind === "triage" && <div className="box"><b>Request → manager splits it up</b><br /><span className="hint">{m?.name ?? "The manager"} doesn’t do this themselves. They hand it to the right staff as new tasks.</span></div>}
        <div className="box">
          <div className="row" style={{ fontSize: 12, color: "var(--muted)" }}>
            <span className="spacer">{t.status === "done" ? "Finished " + ago(t.finished_at) : t.status === "doing" ? "Started " + ago(t.started_at) : waits ? `Waiting on ${waits} task${waits > 1 ? "s" : ""}` : "Ready to start"}</span>
            <span className="tnum">{t.status === "done" ? 100 : t.progress}%</span>
          </div>
          <div className="meter"><i style={{ width: `${t.status === "done" ? 100 : t.progress}%` }} /></div>
        </div>
        {sigs.map((s) => <SignalBox key={s.id} s={s} />)}
        <RunsOn runnerId={runner} goal={g} note={t.kind === "owner_change"
          ? `${personName(b, t.attributed_user_id)} approves this change, so it runs on their computer and edits their folder in place.`
          : `For ${personName(b, t.attributed_user_id)}, with ${signInLabel(b, runner)}.`} />
        {m && (
          <div className="sb-sec">
            <span className="lbl">Assigned to</span>
            <button className="trow" onClick={() => ui.openChat(m.id)}>
              <Avatar name={m.name} color={m.avatar_color} size={28} />
              <span className="spacer"><b>{m.name}</b><br />
                <span className="hint">{m.kind === "manager" ? "Manager" : skillLabel(m.skill ?? "")}{t.borrowed && g ? ", helping " + (visibleMember(b, g.manager_id)?.name ?? "a manager") : ""}, {memberStatus(b, m).t}</span>
              </span>
              <span className="pill">Chat</span>
            </button>
          </div>
        )}
        {t.kind === "triage" && (
          <div className="sb-sec">
            <span className="lbl">Tasks it created</span>
            {b.tasks.filter((x) => x.parent_task_id === t.id).map((x) => <TaskRow key={x.id} t={x} nested />)}
            {!b.tasks.some((x) => x.parent_task_id === t.id) && <p className="hint">None yet.</p>}
          </div>
        )}
        {steps.length > 0 && (
          <div className="sb-sec">
            <span className="lbl">Steps</span>
            <ul className="steps-list">
              {steps.map((st, i) => {
                const done = st.done || t.status === "done";
                return <li key={i} className={`${done ? "done" : ""}${i === t.current_step && t.status === "doing" ? " cur" : ""}`}><span className="ck">{done ? "✓" : ""}</span>{st.title}</li>;
              })}
            </ul>
          </div>
        )}
        {t.description && <div className="sb-sec"><span className="lbl">Details</span><span style={{ whiteSpace: "pre-wrap" }}>{t.description}</span></div>}
        {t.summary && <div className="sb-sec"><span className="lbl">Result</span><span>{t.summary}</span></div>}
        <dl className="kv">
          <dt>Started</dt><dd>{t.started_at ? clock(t.started_at) : "Not yet"}</dd>
          <dt>Finished</dt><dd>{t.finished_at ? clock(t.finished_at) : "Not yet"}</dd>
          <dt>Works in</dt><dd className="mono" style={{ fontSize: 12 }}>{t.required_dirs.length ? t.required_dirs.map((d) => <div key={d.directory_id}>{repoBranch(d.directory_id)}</div>) : "Workspace notes only"}</dd>
          <dt>Can reach</dt><dd style={{ fontSize: 12.5 }}>{reach || "Workspace notes only"}</dd>
        </dl>
        <div className="sb-sec">
          <span className="lbl">Waits on</span>
          {t.depends_on.length ? t.depends_on.map((d) => findTask(b, d)).filter(Boolean).map((x) => <TaskRow key={x!.id} t={x!} />) : <span className="hint">Nothing</span>}
        </div>
        {unlocks.length > 0 && <div className="sb-sec"><span className="lbl">Unlocks</span>{unlocks.map((x) => <TaskRow key={x.id} t={x} />)}</div>}
        <Attachments taskId={t.id} />
        <div className="sb-sec">
          <span className="lbl">Outputs</span>
          {outs.length ? (
            <div className="thumbs">
              {outs.map((o) => <button key={o.id} className={`fruit${o.status === "review" ? " review" : o.status === "approved" ? " approved" : ""}`} onClick={() => ui.openViewer(o.id)} title={o.title}><OutputThumb o={o} /></button>)}
            </div>
          ) : <p className="hint">Nothing yet.</p>}
        </div>
        <div className="sb-sec">
          <span className="lbl">Activity</span>
          {taskLogs.length ? (
            <ul className="timeline">{taskLogs.map((l) => <li key={l.id}><span className="mono hint" style={{ fontSize: 11 }}>{clock(l.at)}</span> {l.text.slice(0, 300)}</li>)}</ul>
          ) : <p className="hint">Nothing yet.</p>}
        </div>
        <div className="sb-sec">
          <span className="lbl">Comments</span>
          {(comments ?? []).map((c) => {
            const author = c.author_member_id ? visibleMember(b, c.author_member_id) : undefined;
            const name = author?.name ?? personName(b, c.author_user_id);
            const color = author?.avatar_color ?? b.people.find((p) => p.id === c.author_user_id)?.avatar_color ?? "var(--line)";
            return (
              <div className="comment" key={c.id}>
                <Avatar name={name} color={color} size={24} />
                <span><b>{name}</b> <span className="mono hint" style={{ fontSize: 11 }}>{clock(c.created_at)}</span><br />{c.text}</span>
              </div>
            );
          })}
          {comments && !comments.length && <p className="hint">No comments yet.</p>}
        </div>
      </PanelBody>
      <PanelFoot>
        <form className="stack-sm" style={{ width: "100%", gap: 6 }} onSubmit={send}>
          <label className="lbl" htmlFor="comment-input">Comment for {m?.name ?? "the agent"}</label>
          <div className="row">
            <input className="input" id="comment-input" value={text} onChange={(e) => setText(e.target.value)} placeholder="Add context or a change" />
            <button className="btn btn-primary btn-sm" type="submit">Post</button>
          </div>
        </form>
      </PanelFoot>
    </>
  );
}
