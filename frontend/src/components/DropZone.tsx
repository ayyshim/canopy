"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { clipboardName, pickFiles, pickMessage } from "@/lib/files";
import { useUI } from "@/lib/board/ui";

const hasFiles = (dt: DataTransfer | null) => !!dt && [...dt.types].includes("Files");

// Zones on screen, most recently used last: a pasted file goes to the one in use.
const zones: { el: HTMLElement; take: (f: File[]) => void }[] = [];
const touch = (el: HTMLElement) => {
  const i = zones.findIndex((z) => z.el === el);
  if (i >= 0) zones.push(...zones.splice(i, 1));
};

/** Keep the browser from opening a file that is dropped outside a zone. Call once per page. */
export function useBlockStrayDrops() {
  useEffect(() => {
    const stop = (e: DragEvent) => {
      if (hasFiles(e.dataTransfer) && !(e.target as HTMLElement | null)?.closest?.("[data-dropzone]")) {
        e.preventDefault();
        if (e.dataTransfer && e.type === "dragover") e.dataTransfer.dropEffect = "none";
      }
    };
    window.addEventListener("dragover", stop);
    window.addEventListener("drop", stop);
    // Pasted files and screenshots go to the zone that was last used.
    const paste = (e: ClipboardEvent) => {
      const files = [...(e.clipboardData?.files ?? [])];
      const z = zones[zones.length - 1];
      if (!files.length || !z) return;
      e.preventDefault();
      const now = new Date();
      z.take(files.map((f, i) => new File([f], clipboardName(f, i, now), { type: f.type })));
    };
    document.addEventListener("paste", paste);
    return () => {
      window.removeEventListener("dragover", stop);
      window.removeEventListener("drop", stop);
      document.removeEventListener("paste", paste);
    };
  }, []);
}

/**
 * DropZone wraps an attachment field: dropped and pasted files go to onFiles, after the size and
 * count rules. have is how many files the field already holds. Buttons inside keep working.
 */
export function DropZone({ onFiles, have = 0, busy = false, label = "Attachments", className = "", children }: {
  onFiles: (files: File[]) => void;
  have?: number;
  busy?: boolean;
  label?: string;
  className?: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [over, setOver] = useState(false);
  const depth = useRef(0);
  const latest = useRef({ onFiles, have, busy });
  useEffect(() => { latest.current = { onFiles, have, busy }; });

  const take = (list: File[]) => {
    const { onFiles: fn, have: h, busy: bz } = latest.current;
    if (bz || !list.length) return;
    const r = pickFiles(list, h);
    const msg = pickMessage(r);
    if (msg) useUI.getState().toast(msg, "warn");
    if (r.accepted.length) fn(r.accepted);
  };
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const z = { el, take };
    zones.push(z);
    return () => { zones.splice(zones.indexOf(z), 1); };
  }, []);

  return (
    <div
      ref={ref}
      data-dropzone
      role="group"
      aria-label={`${label}. Drop or paste files here, or use the button.`}
      className={`dropzone${over ? " drag" : ""} ${className}`}
      onPointerEnter={() => ref.current && touch(ref.current)}
      onFocus={() => ref.current && touch(ref.current)}
      onDragEnter={(e) => { if (!hasFiles(e.dataTransfer)) return; e.preventDefault(); depth.current++; setOver(true); }}
      onDragOver={(e) => { if (!hasFiles(e.dataTransfer)) return; e.preventDefault(); e.dataTransfer.dropEffect = busy ? "none" : "copy"; }}
      onDragLeave={() => { depth.current = Math.max(0, depth.current - 1); if (!depth.current) setOver(false); }}
      onDrop={(e) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        depth.current = 0;
        setOver(false);
        // Folders arrive as empty files; skip them and say so.
        const items = [...e.dataTransfer.items];
        const files = [...e.dataTransfer.files].filter((_, i) => items[i]?.webkitGetAsEntry?.()?.isDirectory !== true);
        if (files.length < e.dataTransfer.files.length) useUI.getState().toast("Folders can’t be attached. Drop files instead.", "warn");
        take(files);
      }}
    >
      {children}
      {over && <div className="dz-hint" aria-hidden="true">{busy ? "Uploading…" : "Drop files to attach"}</div>}
    </div>
  );
}
