"use client";

import { useState } from "react";
import { moveGoal, resolveSignal } from "@/lib/board/actions";
import {
  computer, directory, firstName, goalRepos, isAdmin, payload, personName, signalsForTask, task as findTask, visibleMember, whyWaiting,
} from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { Board, GoalView, SignalView, TaskView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Avatar } from "../icons";

export const shortTitle = (s: string) => (s.length > 34 ? s.slice(0, 32) + "…" : s);

export function goalNeeds(b: Board, g: GoalView) {
  return g.status === "ready" || b.signals.some((s) => s.task_id && findTask(b, s.task_id)?.goal_id === g.id);
}

export function goalLabel(b: Board, g: GoalView) {
  if (g.status === "ready") return goalRepos(b, g.id).length ? "Ready to ship" : "Ready to close";
  return ({ done: "Done", planning: "Planning" } as Record<string, string>)[g.status] ?? "In progress";
}

export function GoalRow({ g }: { g: GoalView }) {
  const b = useB();
  const openSide = useUI((s) => s.openSide);
  return (
    <button className="trow" onClick={() => openSide({ type: "goal", id: g.id })}>
      <span className="gdot" style={{ background: g.color }} />
      <span className="spacer">{g.title}</span>
      {goalNeeds(b, g) && <span className="pill warn">Needs you</span>}
      <span className="hint">{goalLabel(b, g)}</span>
    </button>
  );
}

export function TaskRow({ t }: { t: TaskView }) {
  const b = useB();
  const openSide = useUI((s) => s.openSide);
  const m = visibleMember(b, t.member_id);
  const need = signalsForTask(b, t.id).length > 0;
  return (
    <button className="trow" onClick={() => openSide({ type: "task", id: t.id })}>
      <span className={`dot ${t.status === "done" ? "done" : need ? "need" : t.status === "doing" ? "working" : ""}`} />
      <span className="spacer" style={{ minWidth: 0, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{t.title}</span>
      <span className="hint">{m?.name ?? ""}</span>
      <span className="meter"><i style={{ width: `${t.status === "done" ? 100 : t.progress}%` }} /></span>
    </button>
  );
}

export function StatusPill({ t }: { t: TaskView }) {
  const b = useB();
  if (t.status === "done") return <span className="pill ok">Done</span>;
  if (signalsForTask(b, t.id).length) return <span className="pill warn">Needs you</span>;
  if (t.status === "doing" || t.status === "review") return <span className="pill info">In progress</span>;
  if (t.depends_on.some((d) => findTask(b, d)?.status !== "done")) return <span className="pill">Waiting</span>;
  return <span className="pill">To do</span>;
}

/** SignalBox is a decision shown inside a panel (the same choices as on the tree). */
export function SignalBox({ s }: { s: SignalView }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const [own, setOwn] = useState("");
  const m = visibleMember(b, s.member_id);
  const p = payload(s);
  const act = (a: string, extra?: Record<string, unknown>) => () => resolveSignal(s.id, a, extra);
  let body: React.ReactNode;
  switch (s.type) {
    case "question":
      body = (
        <>
          <b>{m?.name} asks:</b> {p.question}
          <div className="leaves">
            {(p.options ?? []).map((o: string, i: number) => (
              <button key={o} className={"leaf" + (i === p.suggested ? " sugg" : "")} onClick={act("answer", { text: o })}>{o}</button>
            ))}
          </div>
          <form className="row" onSubmit={(e) => { e.preventDefault(); if (own.trim()) resolveSignal(s.id, "answer", { text: own.trim() }).then(() => setOwn("")); }}>
            <input className="input" value={own} onChange={(e) => setOwn(e.target.value)} placeholder="Or write your own answer" aria-label="Your answer" />
            <button className="btn btn-sm" type="submit">Send</button>
          </form>
        </>
      );
      break;
    case "gate":
      body = (
        <>
          <b>{m?.name} wants to run</b><code>{p.summary}</code>
          <div className="row wrap">
            <button className="btn btn-sm btn-primary" onClick={act("allow")}>Allow once</button>
            <button className="btn btn-sm" onClick={act("always")}>Always</button>
            <button className="btn btn-sm btn-danger" onClick={act("deny")}>Deny</button>
          </div>
        </>
      );
      break;
    case "dir_access":
      body = (
        <>
          <b>{m?.name} needs {p.mode === "read_write" ? "write" : "read"} access to</b><code>{p.path}</code><span className="muted">{p.reason}</span>
          <div className="row">
            <button className="btn btn-sm btn-primary" onClick={act("grant")}>Grant access</button>
            <button className="btn btn-sm btn-danger" onClick={act("deny")}>Deny</button>
          </div>
        </>
      );
      break;
    case "review":
      body = (
        <>
          <b>“{p.title}” needs your review</b>
          <div className="row"><button className="btn btn-sm btn-primary" onClick={() => ui.openViewer(p.output_id)}>Open and review</button></div>
        </>
      );
      break;
    case "error":
      body = (
        <>
          <b style={{ color: "var(--red-text)" }}>{p.message}</b>
          <div className="row">
            <button className="btn btn-sm" onClick={act("retry")}>Retry</button>
            <button className="btn btn-sm" onClick={act("skip")}>Skip this step</button>
          </div>
        </>
      );
      break;
    case "owner_change":
      body = (
        <>
          <b>Needs {personName(b, p.owner_user_id)}’s approval</b><span>{p.description}</span>
          {p.owner_user_id === me ? (
            <div className="row">
              <button className="btn btn-sm btn-primary" onClick={act("approve")}>Approve</button>
              <button className="btn btn-sm btn-danger" onClick={act("deny")}>Decline</button>
            </div>
          ) : (
            <span className="muted">{personName(b, p.owner_user_id)} owns {directory(b, p.directory_id)?.name ?? p.path}; you can read it but not change it.</span>
          )}
        </>
      );
      break;
    default:
      body = <span>{s.type}</span>;
  }
  return <div className="box warn">{body}</div>;
}

/** RunsOn shows the computer a goal or task runs on and, when it can't run, how to move it. */
export function RunsOn({ runnerId, note, goal }: { runnerId: string | null | undefined; note?: string; goal?: GoalView }) {
  const b = useB();
  const { me } = useBoardCtx();
  const r = computer(b, runnerId);
  const why = whyWaiting(b, runnerId);
  const canMove = goal && (goal.created_by === me || isAdmin(b));
  const options = canMove ? b.runners.filter((x) => x.online && x.id !== runnerId && (x.owner_user_id === goal!.created_by || x.shared)) : [];
  return (
    <div className="sb-sec">
      <span className="tag">Runs on</span>
      <div className="row">
        <span className={`dot ${r?.online ? "done" : "need"}`} />
        <b>{r?.name ?? "No computer yet"}</b>
        {r?.shared && <span className="pill">Shared</span>}
      </div>
      {note && <span className="hint">{note}</span>}
      {why && (
        <div className="box warn">
          <span>{why}.</span>
          {options.length > 0 && (
            <>
              <div className="row wrap">
                {options.map((o) => <button key={o.id} className="btn btn-sm" onClick={() => moveGoal(goal!.id, o.id, o.name)}>Move to {o.name}</button>)}
              </div>
              <span className="hint">Moving restarts its sessions there from the goal’s notes and progress. Its code comes from the hub’s backup of the branch.</span>
            </>
          )}
          {!r && <span className="hint">Connect a computer in Organization settings → Computers.</span>}
        </div>
      )}
    </div>
  );
}

export function MemberRow({ id, sub }: { id: string; sub?: string }) {
  const b = useB();
  const openChat = useUI((s) => s.openChat);
  const m = visibleMember(b, id);
  if (!m) return null;
  return (
    <button className="trow" onClick={() => openChat(m.id)}>
      <Avatar name={m.name} color={m.avatar_color} size={28} />
      <span className="spacer"><b>{m.name}</b><br /><span className="hint">{sub}</span></span>
      <span className="pill">Chat</span>
    </button>
  );
}

const escHTML = (s: string) => s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

/** markdownHTML renders the small Markdown subset used in notes and instructions. Text is escaped first. */
export function markdownHTML(src: string): string {
  const lines = String(src || "").replace(/\r/g, "").split("\n");
  let out = "", list: "ul" | "ol" | null = null, code = false;
  let buf: string[] = [];
  const inl = (t: string) => escHTML(t).replace(/`([^`]+)`/g, "<code>$1</code>").replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>").replace(/(^|\W)\*([^*]+)\*/g, "$1<i>$2</i>");
  const flush = () => { if (buf.length) { out += "<p>" + buf.map(inl).join(" ") + "</p>"; buf = []; } };
  const endList = () => { if (list) { out += `</${list}>`; list = null; } };
  for (const l of lines) {
    let mm: RegExpMatchArray | null;
    if (/^```/.test(l)) { flush(); endList(); out += code ? "</code></pre>" : "<pre><code>"; code = !code; continue; }
    if (code) { out += escHTML(l) + "\n"; continue; }
    if ((mm = l.match(/^(#{1,3})\s+(.*)/))) { flush(); endList(); out += `<h${mm[1].length}>${inl(mm[2])}</h${mm[1].length}>`; continue; }
    if ((mm = l.match(/^\s*[-*]\s+(.*)/))) { flush(); if (list !== "ul") { endList(); out += "<ul>"; list = "ul"; } out += "<li>" + inl(mm[1]) + "</li>"; continue; }
    if ((mm = l.match(/^\s*\d+[.)]\s+(.*)/))) { flush(); if (list !== "ol") { endList(); out += "<ol>"; list = "ol"; } out += "<li>" + inl(mm[1]) + "</li>"; continue; }
    if (!l.trim()) { flush(); endList(); continue; }
    endList();
    buf.push(l.trim());
  }
  flush();
  endList();
  if (code) out += "</code></pre>";
  return out || '<p class="hint">Nothing written yet.</p>';
}

export function Markdown({ text, className = "mdprev" }: { text: string; className?: string }) {
  return <div className={className} dangerouslySetInnerHTML={{ __html: markdownHTML(text) }} />;
}

export const nameOf = (b: Board, id?: string | null) => firstName(b, id);
