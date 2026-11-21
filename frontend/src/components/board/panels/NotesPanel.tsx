"use client";

import { CopyId, IdTag } from "../IdTag";
import { useState } from "react";
import { deleteNote, saveNote } from "@/lib/board/actions";
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
  if (editing) return <NoteEditor note={editing === "new" ? undefined : editing} done={() => setEditing(null)} />;
  if (note) return <NoteView note={note} edit={() => setEditing(note)} />;
  return (
    <>
      <PanelHead><span className="tag">Notes</span><h2>Notes and decisions</h2></PanelHead>
      <PanelBody>
        <p className="hint">Plans, decisions and goal summaries live in the hub. Every agent reads them before it starts, so knowledge doesn’t depend on anyone’s computer.</p>
        {b.notes.map((n) => (
          <button className="trow" key={n.id} onClick={() => ui.openSide({ type: "notes", id: n.id })}>
            <span className={`pill ${n.kind === "decision" ? "goal" : n.kind === "summary" ? "ok" : ""}`}>{KIND[n.kind]}</span>
            <IdTag kind="note" number={n.number} /><span className="spacer">{n.title}</span><span className="hint">{ago(n.updated_at)}</span>
          </button>
        ))}
        {!b.notes.length && <p className="hint">No notes yet.</p>}
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
  return (
    <>
      <PanelHead><span className="row" style={{ gap: 6 }}><span className="tag">{KIND[note.kind]}</span><IdTag kind="note" number={note.number} /><CopyId kind="note" number={note.number} /></span><h2>{note.title}</h2></PanelHead>
      <PanelBody>
        <span className="hint">By {author ?? "an agent"}, {ago(note.updated_at)}</span>
        <Markdown text={note.body} />
      </PanelBody>
      <PanelFoot>
        {mine && <button className="btn btn-sm btn-danger" onClick={async () => { if (await deleteNote(note.id)) ui.openSide({ type: "notes" }); }}>Delete</button>}
        <span className="spacer" />
        {mine && <button className="btn btn-sm btn-primary" onClick={edit}>Edit</button>}
      </PanelFoot>
    </>
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
        {preview ? <Markdown text={body} /> : <textarea className="input" rows={12} value={body} onChange={(e) => setBody(e.target.value)} />}
      </PanelBody>
      <PanelFoot>
        <button type="button" className="btn btn-sm" onClick={done}>Cancel</button><span className="spacer" />
        <button type="submit" className="btn btn-sm btn-primary">Save</button>
      </PanelFoot>
    </form>
  );
}
