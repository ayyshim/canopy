"use client";

import { useEffect, useRef, useState } from "react";

type Props = {
  label: React.ReactNode;
  ariaLabel: string;
  align?: "left" | "right";
  buttonClass?: string;
  children: (close: () => void) => React.ReactNode;
};

/** A button that opens a menu; closes on Escape, outside click or when an item calls close. */
export function Menu({ label, ariaLabel, align = "left", buttonClass = "btn btn-ghost", children }: Props) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        button.current?.focus();
      }
    };
    const onClick = (e: PointerEvent) => {
      if (!wrap.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onClick);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onClick);
    };
  }, [open]);

  return (
    <div className="menu-wrap" ref={wrap}>
      <button ref={button} className={buttonClass} aria-haspopup="menu" aria-expanded={open} aria-label={ariaLabel} onClick={() => setOpen((o) => !o)}>
        {label}
      </button>
      {open && (
        <div className={align === "right" ? "menu menu-right" : "menu"} role="menu">
          {children(() => setOpen(false))}
        </div>
      )}
    </div>
  );
}
