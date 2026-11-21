"use client";

import { useEffect, useId, useRef } from "react";

type Props = { title: string; onClose: () => void; children: React.ReactNode };

/** A dialog over the page. Escape closes it, Tab stays inside it and focus returns on close. */
export function Modal({ title, onClose, children }: Props) {
  const id = useId();
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const first = box.current?.querySelector<HTMLElement>("input,button,[tabindex]");
    first?.focus();
    return () => opener?.focus();
  }, []);

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Escape") return onClose();
    if (e.key !== "Tab" || !box.current) return;
    const items = [...box.current.querySelectorAll<HTMLElement>("input,button,a[href],[tabindex]")].filter((el) => !el.hasAttribute("disabled"));
    if (!items.length) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()} onKeyDown={onKeyDown}>
      <div ref={box} className="dialog" role="dialog" aria-modal="true" aria-labelledby={id}>
        <h2 id={id}>{title}</h2>
        {children}
      </div>
    </div>
  );
}
