"use client";

import { useState } from "react";
import { ApiError } from "@/lib/api";
import { deleteDraft, giveDraft } from "@/lib/board/actions";
import { clock, computer, isAdmin, personName, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { GoalView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Avatar } from "../icons";
import { Attachments } from "./Attachments";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";

/** DraftPanel shows a saved draft goal: nobody works on it until it is given. */
export function DraftPanel({ g }: { g: GoalView }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const [busy, setBusy] = useState("");
  const m = visibleMember(b, g.manager_id);
  const mine = g.created_by === me || isAdmin(b);

  const give = async () => {
    setBusy("give");
    try {
      await giveDraft(g.id);
      ui.toast(`Goal given to ${m?.name ?? "the manager"}.`, "ok");
    } catch (e) {
      ui.toast(e instanceof ApiError ? e.message : "Something went wrong. Try again.", "error");
    } finally {
      setBusy("");
    }
  };
  const remove = async () => {
    if (!confirm(`Delete the draft “${g.title}”?`)) return;
    setBusy("delete");
    if (await deleteDraft(g.id)) ui.openSide(null);
    setBusy("");
  };

  return (
    <>
      <PanelHead>
        <div className="row" style={{ alignItems: "flex-start" }}>
          <span className="gdot" style={{ background: g.color, width: 14, height: 14, marginTop: 4 }} />
          <div style={{ minWidth: 0 }}><span className="tag">Draft goal</span><h2>{g.title}</h2></div>
        </div>
      </PanelHead>
      <PanelBody>
        <div className="row wrap">
          <span className="pill">Draft</span>
          <span className="hint">Saved by {personName(b, g.created_by)} at {clock(g.created_at)}</span>
        </div>
        <div className="box">
          <span style={{ fontSize: 13 }}>No one is working on this yet. {mine ? "Give it to the manager when it’s ready." : `Waiting for ${personName(b, g.created_by)} to give it.`}</span>
        </div>
        {m && (
          <div className="sb-sec">
            <span className="lbl">Manager</span>
            <div className="row"><Avatar name={m.name} color={m.avatar_color} size={24} /><b>{m.name}</b></div>
          </div>
        )}
        {g.description && <div className="sb-sec"><span className="lbl">Description</span><span style={{ fontSize: 13.5, whiteSpace: "pre-wrap" }}>{g.description}</span></div>}
        {g.done_when && <div className="sb-sec"><span className="lbl">Done when</span><span style={{ fontSize: 13.5 }}>{g.done_when}</span></div>}
        <Attachments goalId={g.id} canAdd={mine} />
        {g.runner_id && <div className="sb-sec"><span className="lbl">Runs on</span><span style={{ fontSize: 13.5 }}>{computer(b, g.runner_id)?.name ?? "A computer"}</span></div>}
      </PanelBody>
      {mine && (
        <PanelFoot>
          <button className="btn btn-sm" disabled={!!busy} onClick={remove}>{busy === "delete" ? "Deleting…" : "Delete"}</button>
          <span className="spacer" />
          <button className="btn btn-sm" disabled={!!busy} onClick={() => ui.openDialog({ kind: "goal", draftId: g.id })}>Edit</button>
          <button className="btn btn-sm btn-primary" disabled={!!busy} onClick={give}>{busy === "give" ? "Giving…" : "Give goal"}</button>
        </PanelFoot>
      )}
    </>
  );
}
