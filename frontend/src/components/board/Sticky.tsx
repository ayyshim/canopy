"use client";

import { useEffect, useRef, useState } from "react";
import { createSticky, patchNote, type NotePatch } from "@/lib/board/actions";
import { camera } from "@/lib/board/camera";
import { isAdmin, visibleMember } from "@/lib/board/derive";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { canEditNote, dragged, editedByAgent, isPrivate, STICKY_COLORS, STICKY_H, STICKY_W, stickyColor, stickyPreview } from "@/lib/stickies";
import type { NoteView } from "@/lib/types";
import { useB, useBoardCtx } from "./context";
import { IdTag } from "./IdTag";
import { Markdown } from "./panels/bits";
import { RefAssist } from "./RefAssist";

const LockIcon = () => (
  <svg width="11" height="11" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true"><rect x="2" y="5.5" width="8" height="5.5" rx="1.3" /><path d="M4 5.5V4a2 2 0 0 1 4 0v1.5" /></svg>
);

// A drag sends its position at most every 300 ms, and once more when it ends.
const timers = new Map<string, ReturnType<typeof setTimeout>>();
function savePosition(id: string, pos: { x: number; y: number }, now = false) {
  clearTimeout(timers.get(id));
  const send = () => {
    timers.delete(id);
    patchNote(id, { board: pos }).catch(() => useUI.getState().toast("Couldn’t move the sticky note.", "error"));
  };
  if (now) send();
  else timers.set(id, setTimeout(send, 300));
}

/** Sticky is a note pinned to the board. Drag to move it, double-click to edit in place. */
export function Sticky({ note }: { note: NoteView }) {
  const b = useB();
  const { me } = useBoardCtx();
  const ui = useUI();
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const [editing, setEditing] = useState(false);
  const [glow, setGlow] = useState(false);
  const drag = useRef<{ px: number; py: number; x: number; y: number; moved: boolean } | null>(null);
  const x = pos?.x ?? note.board_x!, y = pos?.y ?? note.board_y!;
  const mine = canEditNote(note, me, isAdmin(b));
  const agent = editedByAgent(note) ? visibleMember(b, note.edited_by_member_id)?.name ?? "an agent" : null;

  // A short glow when an agent edits the sticky while it is on screen.
  const seen = useRef(note.updated_at);
  useEffect(() => {
    if (seen.current === note.updated_at) return;
    seen.current = note.updated_at;
    if (!note.edited_by_member_id) return;
    const on = setTimeout(() => setGlow(true), 0), off = setTimeout(() => setGlow(false), 1800);
    return () => { clearTimeout(on); clearTimeout(off); };
  }, [note.updated_at, note.edited_by_member_id]);

  const open = () => ui.openSide({ type: "notes", id: note.id });
  const down = (e: React.PointerEvent<HTMLDivElement>) => {
    if (editing || e.button !== 0 || (e.target as HTMLElement).closest("button, a, input, textarea")) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { px: e.clientX, py: e.clientY, x, y, moved: false };
  };
  const move = (e: React.PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    const dx = e.clientX - d.px, dy = e.clientY - d.py;
    if (!d.moved && Math.hypot(dx, dy) < 4) return;
    d.moved = true;
    const p = dragged(d.x, d.y, dx, dy, camera.get().z);
    setPos(p);
    savePosition(note.id, p);
  };
  const up = () => {
    const d = drag.current;
    drag.current = null;
    if (!d) return;
    if (!d.moved) return open();
    if (pos) {
      savePosition(note.id, pos, true);
      const board = useBoard.getState().board;
      if (board) useBoard.setState({ board: { ...board, notes: board.notes.map((n) => (n.id === note.id ? { ...n, board_x: pos.x, board_y: pos.y } : n)) } });
    }
    setPos(null);
  };

  if (editing) {
    return (
      <div className={`sticky c-${stickyColor(note.color)} editing`} style={{ left: x, top: y, width: STICKY_W, minHeight: STICKY_H }}>
        <StickyForm
          note={note}
          onSave={async (p) => { await patchNote(note.id, p).catch(() => useUI.getState().toast("Couldn’t save the sticky note.", "error")); setEditing(false); }}
          onCancel={() => setEditing(false)}
        />
      </div>
    );
  }
  return (
    <div
      className={`sticky c-${stickyColor(note.color)}${isPrivate(note) ? " private" : ""}${glow ? " glow" : ""}${pos ? " dragging" : ""}`}
      style={{ left: x, top: y, width: STICKY_W, height: STICKY_H }}
      role="group"
      aria-label={`Sticky note N-${note.number}, ${note.title}${isPrivate(note) ? ", private" : ""}`}
      tabIndex={0}
      onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={() => { drag.current = null; setPos(null); }}
      onDoubleClick={() => mine && setEditing(true)}
      onKeyDown={(e) => { if (e.target === e.currentTarget && e.key === "Enter") open(); }}
    >
      <div className="st-head">
        <IdTag kind="note" number={note.number} />
        {isPrivate(note) && <span className="st-priv" title="Only you and your agents see this"><LockIcon /> Private</span>}
        <span className="spacer" />
        {mine && <button type="button" className="st-edit" onClick={() => setEditing(true)} aria-label={`Edit sticky note N-${note.number}`}>Edit</button>}
      </div>
      <b className="st-title">{note.title}</b>
      <Markdown className="st-body" text={stickyPreview(note.body)} />
      {agent && <span className="st-by">Edited by {agent}</span>}
    </div>
  );
}

/** StickyDraft is a new sticky being written at one spot of the board. */
export function StickyDraft({ at }: { at: { x: number; y: number } }) {
  const { wsId } = useBoardCtx();
  const ui = useUI();
  return (
    <div className="sticky c-yellow editing" style={{ left: at.x, top: at.y, width: STICKY_W, minHeight: STICKY_H }}>
      <StickyForm
        onSave={async (p) => { if (await createSticky(wsId, { title: p.title ?? "", ...p, board: at })) ui.startSticky(null); }}
        onCancel={() => ui.startSticky(null)}
      />
    </div>
  );
}

/** StickyForm edits a sticky in place. New stickies are private. */
function StickyForm({ note, onSave, onCancel }: { note?: NoteView; onSave: (p: NotePatch) => Promise<unknown>; onCancel: () => void }) {
  const { me } = useBoardCtx();
  const [title, setTitle] = useState(note?.title ?? "");
  const [body, setBody] = useState(note?.body ?? "");
  const [visibility, setVisibility] = useState(note?.visibility ?? "private");
  const [color, setColor] = useState(stickyColor(note?.color));
  const [busy, setBusy] = useState(false);
  const first = useRef<HTMLInputElement>(null);
  useEffect(() => first.current?.focus(), []);
  const author = !note || note.author_user_id === me;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim() || busy) return;
    setBusy(true);
    const p: NotePatch = { title: title.trim(), body, color };
    if (author) p.visibility = visibility;
    await onSave(p);
    setBusy(false);
  };
  return (
    <form className="st-form" onSubmit={submit} onKeyDown={(e) => e.key === "Escape" && onCancel()}>
      <input ref={first} className="st-in" placeholder="Title" aria-label="Title" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={200} />
      <RefAssist><textarea className="st-in" rows={4} placeholder="Write something. Markdown works." aria-label="Text" value={body} onChange={(e) => setBody(e.target.value)} /></RefAssist>
      {author && (
        <div className="seg" role="group" aria-label="Who can see it">
          <button type="button" aria-pressed={visibility === "private"} onClick={() => setVisibility("private")}>Private</button>
          <button type="button" aria-pressed={visibility === "shared"} onClick={() => setVisibility("shared")}>Shared</button>
        </div>
      )}
      <div className="st-colors" role="group" aria-label="Color">
        {STICKY_COLORS.map((c) => <button type="button" key={c} className={`st-sw c-${c}`} aria-label={c} aria-pressed={color === c} onClick={() => setColor(c)} />)}
      </div>
      <div className="row" style={{ gap: 6 }}>
        <button type="button" className="btn btn-sm" onClick={onCancel}>Cancel</button><span className="spacer" />
        <button type="submit" className="btn btn-sm btn-primary" disabled={!title.trim() || busy}>{busy ? "Saving…" : "Save"}</button>
      </div>
    </form>
  );
}
