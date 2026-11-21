"use client";

import { useCallback } from "react";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { itemOf, refLabel, splitRefs, type Ref, type RefKind } from "@/lib/refs";

/** IdTag is the small mono ID of a goal, task or note: G-12, T-34, N-5. */
export function IdTag({ kind, number, className = "" }: { kind: RefKind; number?: number; className?: string }) {
  if (!number) return null;
  const label = refLabel(kind, number);
  return <span className={`idtag mono ${className}`} aria-label={`ID ${label}`}>{label}</span>;
}

/** CopyId copies the ID so it can be pasted into a goal, a chat or a review. */
export function CopyId({ kind, number }: { kind: RefKind; number?: number }) {
  const toast = useUI((s) => s.toast);
  if (!number) return null;
  const label = refLabel(kind, number);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(label);
      toast(`Copied ${label}.`, "ok");
    } catch {
      toast(`Couldn’t copy. The ID is ${label}.`, "warn");
    }
  };
  return <button type="button" className="btn btn-sm" onClick={copy} aria-label={`Copy ID ${label}`}>Copy ID</button>;
}

/** useOpenRef opens a goal, task or note in the details panel and centers it on the board. */
export function useOpenRef() {
  const openSide = useUI((s) => s.openSide);
  const centerOn = useUI((s) => s.centerOn);
  const setGlow = useUI((s) => s.setGlow);
  return useCallback((kind: RefKind, id: string) => {
    openSide({ type: kind === "note" ? "notes" : kind, id });
    if (kind !== "note") { centerOn(id); setGlow(id); }
  }, [openSide, centerOn, setGlow]);
}

/** RefLink is a ref that opens its item. Hidden and unknown refs are never links. */
export function RefLink({ refr, text }: { refr: Ref; text?: string }) {
  const board = useBoard((s) => s.board);
  const open = useOpenRef();
  const item = itemOf(board, refr);
  if (!item) return <>{text ?? refr.label}</>;
  return (
    <button type="button" className="reflink mono" title={`Open ${refr.label}`} onClick={(e) => { e.stopPropagation(); open(item.kind, item.id); }}>
      {text ?? refr.label}
    </button>
  );
}

/** RefText shows plain text with its refs as links. */
export function RefText({ text }: { text: string }) {
  return <>{splitRefs(text).map((p, i) => (typeof p === "string" ? p : <RefLink key={i} refr={p.ref} text={p.text} />))}</>;
}
