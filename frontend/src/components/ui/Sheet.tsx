"use client";

import { useEffect, useRef, type ReactNode } from "react";

/**
 * Sheet is a side panel: 440px on wide screens, the whole screen on phones.
 * The header (title, close button, tabs) and the action bar stay put while the body scrolls.
 */
export function Sheet({ open, onClose, eyebrow, title, tabs, footer, children }: {
  open: boolean; onClose: () => void; eyebrow?: ReactNode; title: ReactNode; tabs?: ReactNode; footer?: ReactNode; children: ReactNode;
}) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", onKey);
    ref.current?.focus();
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <>
      <div className="ui-scrim" onClick={onClose} aria-hidden="true" />
      <aside ref={ref} className="ui-sheet" role="dialog" aria-modal="true" aria-label={typeof title === "string" ? title : undefined} tabIndex={-1}>
        <header className="ui-sheet-h">
          <div className="ui-sheet-top">
            <div className="ui-sheet-title">
              {eyebrow && <div className="hint">{eyebrow}</div>}
              <h2>{title}</h2>
            </div>
            <button className="icon-btn" onClick={onClose} aria-label="Close">✕</button>
          </div>
          {tabs}
        </header>
        <div className="ui-sheet-b">{children}</div>
        {footer && <footer className="ui-sheet-f">{footer}</footer>}
      </aside>
    </>
  );
}
