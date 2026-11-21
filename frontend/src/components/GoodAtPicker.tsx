"use client";

import { useEffect, useId, useMemo, useRef, useState } from "react";
import { skillDesc, skillLabel, useCatalog } from "@/lib/catalog";

type Props = { value: string; onChange: (key: string) => void; disabled?: boolean; id?: string };

/** A searchable "Good at" picker, grouped by category, with the description as a hint. */
export function GoodAtPicker({ value, onChange, disabled, id }: Props) {
  const catalog = useCatalog();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const root = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const listId = useId();

  useEffect(() => {
    if (!open) return;
    search.current?.focus({ preventScroll: true });
    panel.current?.scrollIntoView({ block: "nearest" });
    const down = (e: MouseEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => { document.removeEventListener("mousedown", down); document.removeEventListener("keydown", key); };
  }, [open]);

  const groups = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return catalog;
    return catalog.flatMap((c) => {
      if (c.label.toLowerCase().includes(t)) return [c];
      const subs = c.subs.filter((s) => s.label.toLowerCase().includes(t) || s.desc.toLowerCase().includes(t));
      return subs.length ? [{ ...c, subs }] : [];
    });
  }, [catalog, q]);

  const pick = (k: string) => { onChange(k); setOpen(false); setQ(""); };
  const known = catalog.some((c) => c.subs.some((s) => s.key === value));

  return (
    <div className="gapick" ref={root}>
      <button type="button" id={id} className="input gabtn" disabled={disabled} aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? listId : undefined} onClick={() => setOpen((o) => !o)}>
        <span>{skillLabel(value)}</span><span aria-hidden>▾</span>
      </button>
      {open && (
        <div className="gapanel" id={listId} ref={panel}>
          <input ref={search} className="input" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search skills" aria-label="Search skills" />
          <div className="galist">
            {!known && value && !q && <button type="button" className="gaopt on" onClick={() => pick(value)}><b>{skillLabel(value)}</b><small>{skillDesc(value)}</small></button>}
            {groups.map((c) => (
              <div key={c.key} role="group" aria-label={c.label}>
                <div className="gahead">{c.label}</div>
                {c.subs.map((s) => (
                  <button type="button" key={s.key} className={"gaopt" + (s.key === value ? " on" : "")} aria-pressed={s.key === value} onClick={() => pick(s.key)}>
                    <b>{s.label}</b><small>{s.desc}</small>
                  </button>
                ))}
              </div>
            ))}
            {!groups.length && <p className="hint" style={{ padding: 10 }}>Nothing matches “{q}”</p>}
          </div>
        </div>
      )}
    </div>
  );
}
