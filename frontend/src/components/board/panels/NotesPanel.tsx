"use client";

import { RefAssist } from "../RefAssist";
import { CopyId, IdTag } from "../IdTag";
import { useEffect, useState } from "react";
import { deleteNote, listRevisions, patchNote, restoreRevision, saveNote } from "@/lib/board/actions";
import { camera } from "@/lib/board/camera";
import { canChangeVisibility, canEditNote, centerSpot, isPinned, isPrivate } from "@/lib/stickies";
import type { NoteRevisionView } from "@/lib/core-types";
import { ago, isAdmin, personName, visibleMember } from "@/lib/board/derive";
import { useUI } from "@/lib/board/ui";
import type { NoteView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";
import { Markdown } from "./bits";
import { PanelBody, PanelFoot, PanelHead } from "./Panel";

const KIND: Record<string, string> = { note: "Note", decision: "Decision", plan: "Plan", summary: "Summary" };

/** NotesPanel lists the workspace's notes, or shows one note with editing for its author. */
export function NotesPanel({ id }: { id: string }) {
  const b = useB();
  const ui = useUI();
  const note = id ? b.notes.find((n) => n.id === id) : undefined;
  const [editing, setEditing] = useState<NoteView | "new" | null>(null);
  const [filter, setFilter] = useState<"all" | "private" | "shared">("all");
  const shown = b.notes.filter((n) => filter === "all" || (filter === "private") === isPrivate(n));
  if (editing) return <NoteEditor note={editing === "new" ? undefined : editing} done={() => setEditing(null)} />;
  if (note) return <NoteView note={note} edit={() => setEditing(note)} />;
  return (
    <>
      <PanelHead><span className="tag">Notes</span><h2>Notes and decisions</h2></PanelHead>
      <PanelBody>
        <p className="hint">Plans, decisions and goal summaries live in the hub. Every agent reads them before it starts, so knowledge doesn’t depend on anyone’s computer.</p>
        <div className="notefilter">
          <div className="seg" role="group" aria-label="Show notes">
            {(["all", "private", "shared"] as const).map((f) => (
              <button type="button" key={f} aria-pressed={filter === f} onClick={() => setFilter(f)}>{f === "all" ? "All" : f === "private" ? "Private" : "Shared"}</button>
            ))}
          </div>
        </div>
        {shown.map((n) => (
          <button className="trow" key={n.id} onClick={() => ui.openSide({ type: "notes", id: n.id })}>
            <span className={`pill ${n.kind === "decision" ? "goal" : n.kind === "summary" ? "ok" : ""}`}>{KIND[n.kind]}</span>
            <IdTag kind="note" number={n.number} />
            {isPrivate(n) ? <span className="pill priv">Private</span> : <span className="pill">Shared</span>}
            {isPinned(n) && <span className="pill">On board</span>}
            <span className="spacer">{n.title}</span><span className="hint">{ago(n.updated_at)}</span>
          </button>
        ))}
        {!shown.length && <p className="hint">{b.notes.length ? "No notes match." : "No notes yet."}</p>}
      </PanelBody>
      <PanelFoot><button className="btn btn-sm btn-primary" onClick={() => setEditing("new")}>+ New note</button></PanelFoot>
    </>
  );
}

function NoteView({ note, edit }: { note: NoteView; edit: () => void }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const author = note.author_member_id ? visibleMember(b, note.author_member_id)?.name : personName(b, note.author_user_id);
  const mine = note.author_user_id === me || isAdmin(b);
  const canEdit = canEditNote(note, me, isAdmin(b));
  const change = (p: Parameters<typeof patchNote>[1]) => patchNote(note.id, p).catch(() => ui.toast("Couldn’t change the note.", "error"));
  const pin = () => { const c = camera.get(); const s = centerSpot(c, c.w, c.h); change({ board: s }).then(() => ui.centerOn(note.id)); };
  return (
    <>
      <PanelHead><span className="row" style={{ gap: 6 }}><span className="tag">{KIND[note.kind]}</span><IdTag kind="note" number={note.number} /><CopyId kind="note" number={note.number} /></span><h2>{note.title}</h2></PanelHead>
      <PanelBody>
        <div className="row wrap" style={{ gap: 6 }}>
          {isPrivate(note) ? <span className="pill priv">Private: only you and your agents</span> : <span className="pill">Shared with the workspace</span>}
          {isPinned(note) && <span className="pill">On the board</span>}
        </div>
        <span className="hint">By {author ?? "an agent"}, {ago(note.updated_at)}</span>
        <Markdown text={note.body} />
        {canChangeVisibility(note, me) && (
          <div className="sb-sec"><span className="lbl">Who can see it</span>
            <div className="seg" role="group" aria-label="Who can see it">
              <button type="button" aria-pressed={isPrivate(note)} onClick={() => change({ visibility: "private" })}>Private</button>
              <button type="button" aria-pressed={!isPrivate(note)} onClick={() => change({ visibility: "shared" })}>Shared</button>
            </div>
          </div>
        )}
        {canEdit && (
          <div className="row wrap" style={{ gap: 6 }}>
            {isPinned(note)
              ? <button className="btn btn-sm" onClick={() => change({ board: null })}>Remove from board</button>
              : <button className="btn btn-sm" onClick={pin}>Pin to board</button>}
          </div>
        )}
        <History note={note} canEdit={canEdit} />
      </PanelBody>
      <PanelFoot>
        {mine && <button className="btn btn-sm btn-danger" onClick={async () => { if (await deleteNote(note.id)) ui.openSide({ type: "notes" }); }}>Delete</button>}
        <span className="spacer" />
        {mine && <button className="btn btn-sm btn-primary" onClick={edit}>Edit</button>}
      </PanelFoot>
    </>
  );
}

/** History lists earlier versions of a note, with Restore. */
function History({ note, canEdit }: { note: NoteView; canEdit: boolean }) {
  const b = useB();
  const [revs, setRevs] = useState<NoteRevisionView[] | null>(null);
  useEffect(() => {
    let live = true;
    listRevisions(note.id).then((r) => live && setRevs(r)).catch(() => live && setRevs([]));
    return () => { live = false; };
  }, [note.id, note.updated_at]);
  if (!revs?.length) return null;
  const who = (r: NoteRevisionView) => (r.edited_by_member_id ? visibleMember(b, r.edited_by_member_id)?.name ?? "an agent" : personName(b, r.edited_by_user_id));
  return (
    <div className="sb-sec"><span className="lbl">History</span>
      <ul className="revs">
        {revs.map((r) => (
          <li key={r.rev}>
            <span>{r.title}</span><span className="hint">{who(r)}, {ago(r.created_at)}</span><span className="spacer" />
            {canEdit && <button type="button" className="linkbtn" onClick={() => restoreRevision(note.id, r.rev)} aria-label={`Restore the version from ${ago(r.created_at)}`}>Restore</button>}
          </li>
        ))}
      </ul>
    </div>
  );
}

function NoteEditor({ note, done }: { note?: NoteView; done: () => void }) {
  const { wsId } = useBoardCtx();
  const [title, setTitle] = useState(note?.title ?? "");
  const [body, setBody] = useState(note?.body ?? "");
  const [kind, setKind] = useState(note?.kind === "decision" ? "decision" : "note");
  const [preview, setPreview] = useState(false);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (await saveNote(wsId, { title, body, kind }, note?.id)) done();
  };
  return (
    <form onSubmit={save} style={{ display: "contents" }}>
      <PanelHead><span className="tag">{note ? "Edit note" : "New note"}</span></PanelHead>
      <PanelBody>
        <label className="field"><span>Title</span><input className="input" value={title} onChange={(e) => setTitle(e.target.value)} required /></label>
        <div className="seg" role="group" aria-label="Kind">
          <button type="button" aria-pressed={kind === "note"} onClick={() => setKind("note")}>Note</button>
          <button type="button" aria-pressed={kind === "decision"} onClick={() => setKind("decision")}>Decision</button>
        </div>
        <div className="row"><span className="lbl spacer">Text (Markdown)</span>
          <button type="button" className="linkbtn" onClick={() => setPreview(!preview)}>{preview ? "Edit" : "Preview"}</button></div>
        {preview ? <Markdown text={body} /> : <RefAssist><textarea className="input" rows={12} value={body} onChange={(e) => setBody(e.target.value)} /></RefAssist>}
      </PanelBody>
      <PanelFoot>
        <button type="button" className="btn btn-sm" onClick={done}>Cancel</button><span className="spacer" />
        <button type="submit" className="btn btn-sm btn-primary">Save</button>
      </PanelFoot>
    </form>
  );
}
