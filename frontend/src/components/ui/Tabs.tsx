"use client";

import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

export type TabItem = { id: string; label: string; count?: number };

/** Tabs is an underline tab bar. It scrolls sideways on phones and moves with the arrow keys. */
export function Tabs({ tabs, value, onChange, label, idBase = "tab" }: {
  tabs: TabItem[]; value: string; onChange: (id: string) => void; label: string; idBase?: string;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: KeyboardEvent) => {
    const i = tabs.findIndex((t) => t.id === value);
    const to = e.key === "ArrowRight" ? (i + 1) % tabs.length : e.key === "ArrowLeft" ? (i - 1 + tabs.length) % tabs.length
      : e.key === "Home" ? 0 : e.key === "End" ? tabs.length - 1 : -1;
    if (to < 0) return;
    e.preventDefault();
    onChange(tabs[to].id);
    refs.current[to]?.focus();
  };
  return (
    <div className="ui-tabs" role="tablist" aria-label={label} onKeyDown={onKey}>
      {tabs.map((t, i) => (
        <button
          key={t.id}
          ref={(el) => { refs.current[i] = el; }}
          role="tab"
          id={`${idBase}-${t.id}`}
          aria-selected={t.id === value}
          aria-controls={`${idBase}-panel-${t.id}`}
          tabIndex={t.id === value ? 0 : -1}
          onClick={() => onChange(t.id)}
        >
          {t.label}
          {t.count != null && <span className="ui-tab-n">{t.count}</span>}
        </button>
      ))}
    </div>
  );
}

/** TabPanel is the body of the selected tab. */
export function TabPanel({ id, value, idBase = "tab", children }: { id: string; value: string; idBase?: string; children: ReactNode }) {
  if (id !== value) return null;
  return <div role="tabpanel" id={`${idBase}-panel-${id}`} aria-labelledby={`${idBase}-${id}`}>{children}</div>;
}

/** useTab keeps the selected tab, and remembers the last one per panel type in this browser. */
export function useTab(panel: string, first: string, valid: string[]): [string, (id: string) => void] {
  const key = `canopy-tab-${panel}`;
  const [tab, setTab] = useState(first);
  useEffect(() => {
    try {
      const saved = localStorage.getItem(key);
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (saved && valid.includes(saved)) setTab(saved);
    } catch { /* storage blocked */ }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  const set = useCallback((id: string) => {
    setTab(id);
    try { localStorage.setItem(key, id); } catch { /* storage blocked */ }
  }, [key]);
  return [tab, set];
}
