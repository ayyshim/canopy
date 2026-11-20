"use client";

import { useState } from "react";
import { task as findTask, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { OutputView } from "@/lib/types";
import { useB } from "../context";
import { KIND_LABEL, Thumb } from "../icons";

const KINDS = ["all", "image", "recording", "document", "diff", "report"];

/** Tray lists every output the viewer can see, by task or newest first. */
export function Tray() {
  const b = useB();
  const ui = useUI();
  const [kind, setKind] = useState("all");
  const [group, setGroup] = useState<"task" | "new">("task");
  if (!ui.tray) return null;
  const outs = b.outputs.filter((o) => kind === "all" || o.kind === kind);
  const count = (k: string) => b.outputs.filter((o) => k === "all" || o.kind === k).length;
  const latest = (o: OutputView) => o.versions[o.versions.length - 1]?.created_at ?? o.created_at;

  const card = (o: OutputView) => {
    const isNew = (b.seen[o.id] ?? 0) < o.versions.length;
    return (
      <div className="ocard" key={o.id} role="button" tabIndex={0} onClick={() => ui.openViewer(o.id)} onKeyDown={(e) => e.key === "Enter" && ui.openViewer(o.id)}>
        <div className="thumb"><Thumb kind={o.kind} /><span className="kind">{KIND_LABEL[o.kind]}</span></div>
        <div className="meta">
          <b>{o.title}</b>
          <span className="by">{visibleMember(b, o.member_id)?.name ?? "An agent"}, version {o.versions.length}</span>
          {o.status === "review" ? <span className="pill warn" style={{ alignSelf: "flex-start" }}>Needs your review</span>
            : o.status === "approved" ? <span className="pill ok" style={{ alignSelf: "flex-start" }}>Approved</span>
            : o.status === "changes" ? <span className="pill" style={{ alignSelf: "flex-start" }}>Changes asked</span>
            : isNew ? <span className="pill info" style={{ alignSelf: "flex-start" }}>New</span> : null}
        </div>
      </div>
    );
  };

  let body: React.ReactNode;
  if (group === "task") {
    const byTask = new Map<string, OutputView[]>();
    for (const o of outs) byTask.set(o.task_id ?? "", [...(byTask.get(o.task_id ?? "") ?? []), o]);
    body = [...byTask.entries()].map(([tid, list]) => (
      <div className="lane" key={tid}>
        <h3>{findTask(b, tid)?.title ?? "A removed task"}
          <button className="linkbtn" onClick={() => { ui.centerOn(tid); ui.setGlow(tid); }}>Show on tree</button></h3>
        <div className="ocards">{list.map(card)}</div>
      </div>
    ));
  } else {
    body = <div className="ocards">{outs.slice().sort((a, c) => latest(c).localeCompare(latest(a))).map(card)}</div>;
  }

  return (
    <section className="tray" aria-label="Outputs">
      <div className="tray-h">
        <h2>Outputs</h2>
        <span className="muted" style={{ fontSize: 13 }}>{b.outputs.length} from agents you can see</span>
        <div className="row wrap" style={{ gap: 6 }} role="group" aria-label="Filter by type">
          {KINDS.map((k) => (
            <button key={k} className="chip-f" aria-pressed={kind === k} onClick={() => setKind(k)}>{k === "all" ? "All" : KIND_LABEL[k] + "s"} {count(k)}</button>
          ))}
        </div>
        <span className="grow" />
        <div className="seg" role="group" aria-label="Group outputs">
          <button aria-pressed={group === "task"} onClick={() => setGroup("task")}>By task</button>
          <button aria-pressed={group === "new"} onClick={() => setGroup("new")}>Newest</button>
        </div>
        <button className="icon-btn" onClick={() => ui.setTray(false)} aria-label="Close outputs">✕</button>
      </div>
      <div className="tray-body">
        {outs.length ? body : <p className="muted" style={{ padding: "22px 4px" }}>No outputs yet. They appear here and on the tree as agents make them.</p>}
      </div>
    </section>
  );
}
