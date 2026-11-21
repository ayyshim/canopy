"use client";

import { useState } from "react";
import { resolveSignal } from "@/lib/board/actions";
import { clock, handoverBrief, handoverReview, isAdmin, personName, visibleMember } from "@/lib/board/derive";
import type { GoalView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { fileURL, useText } from "../outputs/Viewer";
import { Markdown } from "./bits";
import { goalClosed } from "../../../lib/goalStatus";

/** HandoverBrief shows the latest handover.md of a goal and lets its person approve it or ask for changes. */
export function HandoverBrief({ g }: { g: GoalView }) {
  const b = useB();
  const { me } = useBoardCtx();
  const [changes, setChanges] = useState(false);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const o = handoverBrief(b, g.id);
  const n = o?.versions.length ?? 0;
  const text = useText(o ? fileURL(o, n) : "", !!o);
  if (!o) return null;
  const sig = handoverReview(b, g.id);
  const can = g.created_by === me || isAdmin(b);
  const m = visibleMember(b, g.manager_id);
  const approved = !!g.handover_approved_by;
  const waiting = !approved && o.status !== "changes" && !goalClosed(g);

  const send = async (action: string, extra?: Record<string, unknown>) => {
    if (!sig) return;
    setBusy(true);
    const ok = await resolveSignal(sig.id, action, extra);
    setBusy(false);
    if (ok) { setChanges(false); setNote(""); }
  };

  return (
    <div className={`box${approved ? " ok" : sig ? " warn" : ""}`}>
      <b>Handover brief</b>
      <div className="mdprev" style={{ maxHeight: 220 }}>
        {text === null ? <p className="hint">Loading…</p> : <Markdown text={text} className="md" />}
      </div>
      {approved && <span className="hint">Approved by {personName(b, g.handover_approved_by)} at {clock(g.handover_approved_at)}.</span>}
      {!approved && o.status === "changes" && !sig && <span className="hint">{m?.name ?? "The manager"} is revising the brief after the changes asked.</span>}
      {sig && can && (changes ? (
        <>
          <label className="field"><span>What should change?</span><textarea className="textarea" rows={3} value={note} onChange={(e) => setNote(e.target.value)} /></label>
          <div className="row wrap">
            <button className="btn btn-sm btn-primary" disabled={busy || !note.trim()} onClick={() => send("changes", { note })}>Send to {m?.name ?? "the manager"}</button>
            <button className="btn btn-sm" onClick={() => setChanges(false)}>Cancel</button>
          </div>
        </>
      ) : (
        <div className="row wrap">
          <button className="btn btn-sm btn-primary" disabled={busy} onClick={() => send("approve")}>Approve</button>
          <button className="btn btn-sm" onClick={() => setChanges(true)}>Ask for changes</button>
        </div>
      ))}
      {waiting && !can && <span className="hint">Waiting for {personName(b, g.created_by)} to review the brief.</span>}
    </div>
  );
}
