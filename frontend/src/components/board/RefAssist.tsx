"use client";

import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { refQueryAt } from "@/lib/refs";
import { IdTag } from "./IdTag";
import { useBoardCtx } from "./context";

type Item = { ref: string; type: "goal" | "task" | "note"; id: string; number: number; title: string; status: string };
type Field = HTMLInputElement | HTMLTextAreaElement;
type Open = { field: Field; start: number; end: number; query: string; left: number; top: number; up: boolean };

const isField = (t: EventTarget | null): t is Field => t instanceof HTMLTextAreaElement || (t instanceof HTMLInputElement && /^(text|search|)$/.test(t.type));

/** setValue changes a field the way typing does, so React state follows. */
function setValue(el: Field, value: string, caret: number) {
  const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, "value")?.set?.call(el, value);
  el.setSelectionRange(caret, caret);
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

/**
 * RefAssist wraps text fields. Typing # opens a list of goals, tasks and notes; Up, Down and Enter
 * choose, Esc closes, and the ref (T-34) is put in the text.
 */
export function RefAssist({ children, as: Tag = "div" }: { children: React.ReactNode; as?: "div" | "span" }) {
  const { wsId } = useBoardCtx();
  const [open, setOpen] = useState<Open | null>(null);
  const [items, setItems] = useState<Item[]>([]);
  const [sel, setSel] = useState(0);
  const seq = useRef(0);
  const q = open?.query;

  useEffect(() => {
    if (q === undefined || !wsId) return;
    const mine = ++seq.current;
    const t = setTimeout(() => {
      api<{ items: Item[] }>("GET", `/workspaces/${wsId}/refs?q=${encodeURIComponent(q)}`).then(
        (r) => { if (mine === seq.current) { setItems(r.items); setSel(0); } },
        () => { if (mine === seq.current) setItems([]); },
      );
    }, 120);
    return () => clearTimeout(t);
  }, [q, wsId]);

  const close = () => { seq.current++; setOpen(null); setItems([]); };

  const look = (el: Field) => {
    const caret = el.selectionStart ?? el.value.length;
    const hit = refQueryAt(el.value, caret);
    if (!hit) return close();
    const r = el.getBoundingClientRect();
    const room = window.innerHeight - r.bottom;
    const up = room < 230 && r.top > room;
    setOpen({ field: el, start: hit.start, end: caret, query: hit.query, left: Math.max(8, Math.min(r.left, window.innerWidth - 260)),
      top: up ? r.top - 4 : r.bottom + 4, up });
  };

  const pick = (it: Item) => {
    if (!open) return;
    const v = open.field.value;
    const text = it.ref + " ";
    setValue(open.field, v.slice(0, open.start) + text + v.slice(open.end), open.start + text.length);
    close();
    open.field.focus();
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (!open) return;
    const stop = () => { e.preventDefault(); e.stopPropagation(); };
    if (e.key === "Escape") { stop(); close(); }
    else if (e.key === "ArrowDown" && items.length) { stop(); setSel((sel + 1) % items.length); }
    else if (e.key === "ArrowUp" && items.length) { stop(); setSel((sel - 1 + items.length) % items.length); }
    else if ((e.key === "Enter" || e.key === "Tab") && items[sel] && !e.shiftKey && !e.nativeEvent.isComposing) { stop(); pick(items[sel]); }
  };

  return (
    <Tag className="refwrap" onInput={(e) => isField(e.target) && look(e.target)} onKeyDownCapture={onKey}
      onBlur={() => setTimeout(close, 150)} onClickCapture={(e) => { if (isField(e.target) && open) look(e.target); }}>
      {children}
      {open && (
        <div className="refpick" role="listbox" aria-label="Goals, tasks and notes" style={{ position: "fixed", left: open.left, top: open.top, transform: open.up ? "translateY(-100%)" : undefined }}
          onMouseDown={(e) => e.preventDefault()}>
          {items.length ? items.map((it, i) => (
            <button type="button" role="option" key={it.id} aria-selected={i === sel} onMouseEnter={() => setSel(i)} onClick={() => pick(it)}>
              <IdTag kind={it.type} number={it.number} /><span className="rt">{it.title}</span><span className="rs">{it.status}</span>
            </button>
          )) : <div className="none">No match. Try a number or a word from the title.</div>}
        </div>
      )}
    </Tag>
  );
}
