"use client";

import { useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useBoard } from "@/lib/board/store";
import { itemOf, refOfHref, remarkRefs } from "@/lib/refs";
import { moveGoal, resolveSignal } from "@/lib/board/actions";
import {
  computer, directory, firstName, goalRepos, isAdmin, payload, personName, signalsForTask, task as findTask, visibleMember, whyWaiting,
} from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { Board, GoalView, SignalView, TaskView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Avatar } from "../icons";
import { RefLink, RefText } from "../IdTag";
import { UsageBadge } from "../../ui";

export const shortTitle = (s: string) => (s.length > 34 ? s.slice(0, 32) + "…" : s);

export function goalNeeds(b: Board, g: GoalView) {
  return g.status === "ready" || b.signals.some((s) => s.task_id && findTask(b, s.task_id)?.goal_id === g.id);
}

export function goalLabel(b: Board, g: GoalView) {
  if (g.status === "ready") return goalRepos(b, g.id).length ? "Ready to ship" : "Ready to close";
  return ({ done: "Done", planning: "Planning", draft: "Draft" } as Record<string, string>)[g.status] ?? "In progress";
}

export function GoalRow({ g }: { g: GoalView }) {
  const b = useB();
  const openSide = useUI((s) => s.openSide);
  return (
    <button className="trow" onClick={() => openSide({ type: "goal", id: g.id })}>
      <span className="gdot" style={{ background: g.color }} />
      <span className="spacer">{g.title}</span>
      {goalNeeds(b, g) && <span className="pill warn">Needs you</span>}
      <UsageBadge usage={g.usage} compact />
      <span className="hint">{goalLabel(b, g)}</span>
    </button>
  );
}

export function TaskRow({ t, nested }: { t: TaskView; nested?: boolean }) {
  const b = useB();
  const kids = nested ? [] : b.tasks.filter((x) => x.parent_task_id === t.id);
  const openSide = useUI((s) => s.openSide);
  const m = visibleMember(b, t.member_id);
  const need = signalsForTask(b, t.id).length > 0;
  return (
    <>
      <button className="trow" onClick={() => openSide({ type: "task", id: t.id })}>
        <span className={`dot ${t.status === "done" ? "done" : need ? "need" : t.status === "doing" ? "working" : ""}`} />
        <span className="spacer" style={{ minWidth: 0, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{t.title}</span>
        {t.kind === "triage" && <span className="pill">Request</span>}
        <span className="hint">{m?.name ?? ""}</span>
        <UsageBadge usage={t.usage} compact />
        <span className="meter"><i style={{ width: `${t.status === "done" ? 100 : t.progress}%` }} /></span>
      </button>
      {kids.map((k) => <div key={k.id} style={{ paddingLeft: 16 }}><TaskRow t={k} nested /></div>)}
    </>
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
          <b>{m?.name} asks:</b> <RefText text={String(p.question ?? "")} />
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
          <b>Needs {personName(b, p.owner_user_id)}’s approval</b><span><RefText text={String(p.description ?? "")} /></span>
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

/** Where a Markdown output's relative paths point. Images and links can name sibling outputs. */
export type MdLinks = {
  /** The file URL for a relative image path, or null when no output has that name. */
  image: (src: string) => string | null;
  /** Opens a relative link in the viewer. Returns false when no output has that name. */
  open: (href: string) => boolean;
  /** True for a relative path (not a web link or an anchor), so a miss can say so. */
  relative: (href: string) => boolean;
};

/** Markdown renders GitHub-style Markdown. Raw HTML is not rendered and only safe URLs are linked. */
export function Markdown({ text, className = "mdprev", links }: { text: string; className?: string; links?: MdLinks }) {
  const board = useBoard((s) => s.board);
  const refPlugin = useMemo(() => remarkRefs((r) => !!itemOf(board, r)), [board]);
  const src = String(text || "").replace(/\r/g, "");
  if (!src.trim()) return <div className={className}><p className="hint">Nothing written yet.</p></div>;
  return (
    <div className={className}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, refPlugin]}
        components={{
          a: ({ href, children }) => {
            if (!href) return <>{children}</>;
            const ref = refOfHref(href);
            if (ref) return <RefLink refr={ref} text={String(children)} />;
            if (links?.relative(href)) {
              return (
                <a href={href} onClick={(e) => { e.preventDefault(); links.open(href); }} title={`Open ${href}`}>{children}</a>
              );
            }
            if (href.startsWith("#")) return <>{children}</>;
            return <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>;
          },
          img: ({ src: s, alt }) => {
            const url = typeof s === "string" ? (links?.relative(s) ? links.image(s) : s) : null;
            // eslint-disable-next-line @next/next/no-img-element
            if (url) return <img src={url} alt={alt ?? ""} loading="lazy" />;
            return <span className="md-missing">{alt || "Image"} (not found{typeof s === "string" && s ? `: ${s}` : ""})</span>;
          },
          table: ({ children }) => <div className="md-table"><table>{children}</table></div>,
        }}
      >
        {src}
      </ReactMarkdown>
    </div>
  );
}

export const nameOf = (b: Board, id?: string | null) => firstName(b, id);
