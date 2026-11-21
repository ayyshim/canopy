"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useUI } from "@/lib/board/ui";
import { visibleMember } from "@/lib/board/derive";
import { isPrototype } from "@/lib/prototype";
import {
  applyFm, clearFilters, counts, defaultFm, fromSearch, groupRows, isFiltered, SIZE_LABEL, statusOf, stripSearch, toSearch,
  type Fm, type Group, type Row, type Scope, type Sort,
} from "@/lib/outputs/filter";
import type { OutputView } from "@/lib/types";
import { useB } from "../context";
import { KIND_LABEL } from "../icons";
import { OutputThumb } from "../OutputThumb";

const TYPES = ["image", "recording", "document", "diff", "report", "prototype"];
const STATUS_LABEL: Record<string, string> = { review: "Needs review", approved: "Approved", changes: "Changes asked", new: "New" };
const SORTS: [Sort, string][] = [
  ["updated-desc", "Updated, newest"], ["updated-asc", "Updated, oldest"], ["created-desc", "Created, newest"], ["created-asc", "Created, oldest"],
  ["title-asc", "Title A to Z"], ["size-desc", "Size, largest"], ["versions-desc", "Most versions"],
];
const typeLabel = (t: string) => (t === "prototype" ? "Prototype" : KIND_LABEL[t] ?? t);
const fmtSize = (n: number) => (n >= 1048576 ? (n / 1048576).toFixed(1) + " MB" : Math.max(1, Math.round(n / 1024)) + " KB");
const fmtDate = (iso: string) => new Date(iso.length === 10 ? iso + "T00:00:00" : iso).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
const ago = (iso: string) => {
  const d = Math.floor((Date.now() - new Date(iso).getTime()) / 864e5);
  return d <= 0 ? "Today" : d === 1 ? "Yesterday" : `${d} days ago`;
};
const toggle = <T,>(a: T[], v: T) => (a.includes(v) ? a.filter((x) => x !== v) : [...a, v]);

/** FileManager browses every output with search, filters, sorting and grouping. The header button opens it. */
export function FileManager() {
  const open = useUI((s) => s.tray);
  return open ? <Manager /> : null;
}

function Manager() {
  const b = useB();
  const ui = useUI();
  const [fm, setFm] = useState<Fm>(() => {
    const preset = useUI.getState().fmPreset;
    return { ...defaultFm(), ...(preset ?? fromSearch(window.location.search) ?? {}) };
  });
  const [sel, setSel] = useState<string | null>(null);
  const [panel, setPanel] = useState(false);
  const [nav, setNav] = useState(false);
  const [openGoals, setOpenGoals] = useState<Set<string>>(new Set());
  const searchRef = useRef<HTMLInputElement>(null);
  const set = (p: Partial<Fm>) => setFm((f) => ({ ...f, ...p }));

  useEffect(() => { useUI.setState({ fmPreset: null }); }, []);

  // The state lives in the address bar, so a reload or a shared link comes back to the same view.
  useEffect(() => {
    const rest = stripSearch(window.location.search);
    const q = [rest, toSearch(fm)].filter(Boolean).join("&");
    window.history.replaceState(window.history.state, "", `${window.location.pathname}?${q}${window.location.hash}`);
  }, [fm]);
  useEffect(() => () => {
    const rest = stripSearch(window.location.search);
    window.history.replaceState(window.history.state, "", window.location.pathname + (rest ? `?${rest}` : "") + window.location.hash);
  }, []);

  const byId = useMemo(() => new Map<string, OutputView>(b.outputs.map((o) => [o.id, o])), [b.outputs]);
  const rows: Row[] = useMemo(() => b.outputs.map((o) => {
    const t = b.tasks.find((x) => x.id === o.task_id);
    const g = t ? b.goals.find((x) => x.id === t.goal_id) : undefined;
    const last = o.versions[o.versions.length - 1];
    return {
      id: o.id, title: o.title, name: o.name, type: isPrototype(o) ? "prototype" : o.kind, status: o.status,
      isNew: (b.seen[o.id] ?? 0) < o.versions.length,
      taskId: t?.id ?? "", taskTitle: t?.title ?? "", goalId: g?.id ?? "", goalTitle: g?.title ?? "", goalActive: !!g && g.status !== "done",
      staffId: o.member_id ?? "", staffName: visibleMember(b, o.member_id)?.name ?? "An agent",
      model: last?.model ?? "", size: last?.size ?? 0, versions: o.versions.length, notes: o.versions.map((v) => v.note).join(" "),
      created: o.created_at, updated: last?.created_at ?? o.created_at,
    };
  }), [b]);
  const rowById = useMemo(() => new Map(rows.map((r) => [r.id, r])), [rows]);

  const list = useMemo(() => applyFm(rows, fm), [rows, fm]);
  const groups = useMemo(() => groupRows(list, fm.group, typeLabel), [list, fm.group]);
  const inScope = useMemo(() => rows.filter((r) => applyFm([r], { ...defaultFm(), scope: fm.scope }).length), [rows, fm.scope]);
  const picked = sel ? rowById.get(sel) : undefined;
  const filtered = isFiltered(fm);

  const close = () => ui.setTray(false);
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => {});
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (useUI.getState().viewer) return;
      const typing = !!(e.target as HTMLElement).closest("input, textarea, select");
      if (e.key === "Escape") {
        if (panel) setPanel(false);
        else if (nav) setNav(false);
        else if (sel) setSel(null);
        else close();
      } else if (e.key === "/" && !typing) { e.preventDefault(); searchRef.current?.focus(); }
    };
    keyRef.current = onKey;
  });
  // Registered once: a re-register would miss the key when an earlier handler re-renders mid-event.
  useEffect(() => {
    const fn = (e: KeyboardEvent) => keyRef.current(e);
    window.addEventListener("keydown", fn);
    return () => window.removeEventListener("keydown", fn);
  }, []);

  const showOnTree = (r: Row) => { close(); if (r.taskId) { ui.centerOn(r.taskId); ui.setGlow(r.taskId); } };
  const reset = (p: Partial<Fm> = {}) => { setFm((f) => ({ ...clearFilters(f), ...p })); setSel(null); setNav(false); };

  /* ---- sidebar ---- */
  const nIn = (fn: (r: Row) => boolean) => inScope.filter(fn).length;
  const only = (a: string[], v: string) => a.length === 1 && a[0] === v;
  const goals = b.goals.filter((g) => fm.scope === "all" || (fm.scope === "active") === (g.status !== "done")).filter((g) => rows.some((r) => r.goalId === g.id));
  const staff = [...new Set(rows.map((r) => r.staffId))].filter(Boolean);
  const staffName = (id: string) => visibleMember(b, id)?.name ?? "An agent";
  const side = (
    <nav className={"fm-side" + (nav ? " open" : "")} aria-label="Folders">
      <h4>Library</h4>
      <button className={"fm-si" + (!filtered ? " on" : "")} onClick={() => reset()}><span className="t">All outputs</span><span className="n">{inScope.length}</span></button>
      <button className={"fm-si" + (only(fm.statuses, "review") ? " on" : "")} onClick={() => reset({ statuses: ["review"] })}><span className="t">Needs review</span><span className="n">{nIn((r) => statusOf(r) === "review")}</span></button>
      <button className={"fm-si" + (only(fm.statuses, "new") ? " on" : "")} onClick={() => reset({ statuses: ["new"] })}><span className="t">New</span><span className="n">{nIn((r) => statusOf(r) === "new")}</span></button>
      <h4>Goals and tasks</h4>
      {goals.length === 0 && <p className="fm-none">No goals here yet.</p>}
      {goals.map((g) => {
        const isOpen = openGoals.has(g.id) || fm.goals.includes(g.id) || fm.tasks.some((id) => b.tasks.find((t) => t.id === id)?.goal_id === g.id);
        const tasks = b.tasks.filter((t) => t.goal_id === g.id && rows.some((r) => r.taskId === t.id));
        return (
          <div key={g.id}>
            <div className={"fm-si" + (only(fm.goals, g.id) && !fm.tasks.length ? " on" : "")}>
              <button className="car" aria-label={isOpen ? "Collapse" : "Expand"} aria-expanded={isOpen} onClick={() => setOpenGoals((s) => { const n = new Set(s); if (!n.delete(g.id)) n.add(g.id); return n; })}>{isOpen ? "▾" : "▸"}</button>
              <button className="main" onClick={() => reset({ goals: [g.id] })}>
                <span className="gdot" style={{ background: g.color }} /><span className="t" title={g.title}>{g.title}</span><span className="n">{nIn((r) => r.goalId === g.id)}</span>
              </button>
            </div>
            {isOpen && tasks.map((t) => (
              <button key={t.id} className={"fm-si sub" + (only(fm.tasks, t.id) ? " on" : "")} onClick={() => reset({ tasks: [t.id] })}>
                <span className="t" title={t.title}>{t.title}</span><span className="n">{nIn((r) => r.taskId === t.id)}</span>
              </button>
            ))}
          </div>
        );
      })}
      <h4>Staff</h4>
      {staff.map((id) => (
        <button key={id} className={"fm-si" + (only(fm.staff, id) ? " on" : "")} onClick={() => reset({ staff: [id] })}>
          <span className="t">{staffName(id)}</span><span className="n">{nIn((r) => r.staffId === id)}</span>
        </button>
      ))}
    </nav>
  );

  /* ---- chips ---- */
  const chips: { text: string; drop: () => void }[] = [];
  const listChip = (field: "goals" | "tasks" | "staff" | "types" | "statuses" | "models" | "sizes", label: string, name: (v: string) => string) =>
    (fm[field] as string[]).forEach((v) => chips.push({ text: `${label}: ${name(v)}`, drop: () => set({ [field]: (fm[field] as string[]).filter((x) => x !== v) } as Partial<Fm>) }));
  listChip("goals", "Goal", (v) => b.goals.find((g) => g.id === v)?.title ?? "Removed goal");
  listChip("tasks", "Task", (v) => b.tasks.find((t) => t.id === v)?.title ?? "Removed task");
  listChip("staff", "Staff", staffName);
  listChip("types", "Type", typeLabel);
  listChip("statuses", "Status", (v) => STATUS_LABEL[v] ?? v);
  listChip("models", "Model", (v) => v);
  listChip("sizes", "Size", (v) => SIZE_LABEL[v as keyof typeof SIZE_LABEL]);
  for (const k of ["created", "updated"] as const) {
    const r = fm[k];
    if (r.from || r.to) chips.push({ text: `${k === "created" ? "Created" : "Updated"}: ${r.from ? fmtDate(r.from) : "any"} to ${r.to ? fmtDate(r.to) : "any"}`, drop: () => set({ [k]: { from: "", to: "" } } as Partial<Fm>) });
  }
  if (fm.q.trim()) chips.push({ text: `Search: ${fm.q.trim()}`, drop: () => set({ q: "" }) });

  /* ---- filter panel ---- */
  const check = (field: "goals" | "tasks" | "staff" | "types" | "statuses" | "models" | "sizes", opts: [string, string][]) => {
    const n = counts(rows, fm, field);
    return opts.map(([v, label]) => (
      <label key={v}>
        <input type="checkbox" checked={(fm[field] as string[]).includes(v)} onChange={() => set({ [field]: toggle(fm[field] as string[], v) } as Partial<Fm>)} />
        <span className="t">{label}</span><span className="n">{n.get(v) ?? 0}</span>
      </label>
    ));
  };
  const range = (k: "created" | "updated") => {
    const r = fm[k];
    const day = (d: number) => { const x = new Date(); x.setDate(x.getDate() - (d - 1)); return new Date(x.getTime() - x.getTimezoneOffset() * 6e4).toISOString().slice(0, 10); };
    return (
      <div className="dr">
        <label>From <input type="date" value={r.from} onChange={(e) => set({ [k]: { ...r, from: e.target.value } } as Partial<Fm>)} /></label>
        <label>To <input type="date" value={r.to} onChange={(e) => set({ [k]: { ...r, to: e.target.value } } as Partial<Fm>)} /></label>
        <div className="quick">{[[1, "Today"], [7, "7 days"], [30, "30 days"]].map(([d, l]) => <button key={l} onClick={() => set({ [k]: { from: day(d as number), to: day(1) } } as Partial<Fm>)}>{l}</button>)}</div>
      </div>
    );
  };
  const scopeGoals = b.goals.filter((g) => fm.scope === "all" || (fm.scope === "active") === (g.status !== "done")).filter((g) => rows.some((r) => r.goalId === g.id));
  const scopeTasks = b.tasks.filter((t) => scopeGoals.some((g) => g.id === t.goal_id) && (!fm.goals.length || fm.goals.includes(t.goal_id ?? "")) && rows.some((r) => r.taskId === t.id));
  const models = [...new Set(rows.map((r) => r.model).filter(Boolean))].sort();

  /* ---- results ---- */
  const pill = (r: Row) => {
    const s = statusOf(r);
    return s ? <span className={"pill " + (s === "review" ? "warn" : s === "approved" ? "ok" : s === "new" ? "info" : "")}>{STATUS_LABEL[s]}</span> : null;
  };
  const open_ = (id: string) => setSel((v) => (v === id ? null : id));
  const card = (r: Row) => (
    <button key={r.id} className={"fm-card" + (sel === r.id ? " sel" : "")} onClick={() => open_(r.id)} onDoubleClick={() => ui.openViewer(r.id)}>
      <span className="thumb"><OutputThumb o={byId.get(r.id)!} /><span className="k">{typeLabel(r.type)}</span></span>
      <span className="meta"><b>{r.title}</b><span className="by">{r.staffName}, version {r.versions}, {ago(r.updated)}</span>{pill(r)}</span>
    </button>
  );
  const rowEl = (r: Row) => (
    <button key={r.id} className={"fm-row" + (sel === r.id ? " sel" : "")} onClick={() => open_(r.id)} onDoubleClick={() => ui.openViewer(r.id)}>
      <span className="ic"><OutputThumb o={byId.get(r.id)!} /></span>
      <span className="tt"><b>{r.title}</b><span>{r.staffName}, version {r.versions}</span></span>
      <span className="c h">{r.taskTitle || "No task"}</span><span className="c h">{typeLabel(r.type)}</span>
      <span>{pill(r)}</span><span className="c h">{ago(r.updated)}</span><span className="c h">{fmtSize(r.size)}</span>
    </button>
  );
  const wrap = (rs: Row[]) => fm.view === "grid" ? <div className="fm-cards">{rs.map(card)}</div>
    : <div className="fm-rows"><div className="fm-row hd"><span /><span>Name</span><span className="h">Task</span><span className="h">Type</span><span>Status</span><span className="h">Updated</span><span className="h">Size</span></div>{rs.map(rowEl)}</div>;

  const o = picked ? byId.get(picked.id) : undefined;
  const preview = picked && o && (
    <aside className="fm-prev" aria-label="Preview">
      <button className="icon-btn fm-pclose" onClick={() => setSel(null)} aria-label="Close preview">✕</button>
      <div className="pt"><OutputThumb o={o} /></div>
      <h3>{picked.title}</h3>
      <div className="row wrap" style={{ gap: 6 }}>{pill(picked)}{!picked.goalActive && picked.goalId && <span className="pill">Finished goal</span>}</div>
      <div className="acts">
        <button className="btn btn-primary btn-sm" onClick={() => ui.openViewer(picked.id)}>Open</button>
        {picked.taskId && <button className="btn btn-sm" onClick={() => showOnTree(picked)}>Show on tree</button>}
      </div>
      <dl>
        <dt>Type</dt><dd>{typeLabel(picked.type)}, {picked.name}</dd>
        <dt>Goal</dt><dd>{picked.goalTitle || "None"}</dd>
        <dt>Task</dt><dd>{picked.taskTitle || "None"}</dd>
        <dt>Made by</dt><dd>{picked.staffName}</dd>
        {picked.model && <><dt>Model</dt><dd>{picked.model}</dd></>}
        <dt>Size</dt><dd>{fmtSize(picked.size)}</dd>
        <dt>Created</dt><dd>{fmtDate(picked.created)}</dd>
        <dt>Updated</dt><dd>{fmtDate(picked.updated)} ({ago(picked.updated)})</dd>
      </dl>
      <h5>Versions ({o.versions.length})</h5>
      <div className="fm-vers">
        {o.versions.slice().reverse().map((v, i) => (
          <button key={v.number} className={"ver" + (i === 0 ? " cur" : "")} onClick={() => ui.openViewer(o.id, v.number)}>
            <span className="vh"><b>Version {v.number}</b>{i === 0 && <span className="pill ok">Latest</span>}<span>{fmtDate(v.created_at)}</span></span>
            {(v.note || v.model) && <span className="vn">{[v.note, v.model, fmtSize(v.size)].filter(Boolean).join(", ")}</span>}
          </button>
        ))}
      </div>
    </aside>
  );

  return (
    <div className="fm-wrap" role="dialog" aria-modal="true" aria-label="Outputs" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className="fm">
        <div className="fm-head">
          <button className="icon-btn fm-navbtn" onClick={() => setNav((v) => !v)} aria-label="Folders" aria-expanded={nav}>☰</button>
          <h2>Outputs</h2>
          <div className="fm-search">
            <span aria-hidden="true">⌕</span>
            <input ref={searchRef} type="search" value={fm.q} onChange={(e) => set({ q: e.target.value })} placeholder="Search title, task, goal, staff, notes" aria-label="Search outputs" autoFocus />
          </div>
          <button className="btn btn-sm" onClick={() => setPanel((v) => !v)} aria-expanded={panel}>Filters{chips.length > 0 && <span className="bdg">{chips.length}</span>}</button>
          <button className="icon-btn" onClick={close} aria-label="Close outputs">✕</button>
        </div>
        <div className="fm-tools">
          <div className="seg" role="group" aria-label="Goals to show">
            {([["active", "Active goals"], ["finished", "Finished"], ["all", "All"]] as [Scope, string][]).map(([v, l]) => (
              <button key={v} aria-pressed={fm.scope === v} onClick={() => set({ scope: v, goals: [], tasks: [] })}>{l}</button>
            ))}
          </div>
          <label className="fm-sel">Group
            <select value={fm.group} onChange={(e) => set({ group: e.target.value as Group })}>
              <option value="goal">Goal</option><option value="task">Task</option><option value="staff">Staff</option><option value="type">Type</option><option value="none">None</option>
            </select>
          </label>
          <label className="fm-sel">Sort
            <select value={fm.sort} onChange={(e) => set({ sort: e.target.value as Sort })}>{SORTS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
          </label>
          <span className="grow" />
          <span className="muted fm-count" aria-live="polite">{list.length} of {inScope.length} outputs</span>
          <div className="seg" role="group" aria-label="Layout">
            <button aria-pressed={fm.view === "grid"} onClick={() => set({ view: "grid" })} aria-label="Grid">▦</button>
            <button aria-pressed={fm.view === "list"} onClick={() => set({ view: "list" })} aria-label="List">☰</button>
          </div>
        </div>
        {chips.length > 0 && (
          <div className="fm-chips">
            {chips.map((c) => <span className="fm-chip" key={c.text}>{c.text}<button onClick={c.drop} aria-label={`Remove ${c.text}`}>✕</button></span>)}
            <button className="linkbtn" onClick={() => reset()}>Clear all</button>
          </div>
        )}
        <div className={"fm-body" + (picked ? "" : " noprev")}>
          {side}
          {nav && <div className="fm-scrim" onClick={() => setNav(false)} />}
          <section className="fm-main">
            {list.length === 0 ? (
              <div className="fm-empty">
                <h3>{filtered ? "Nothing matches" : rows.length ? "No outputs here" : "No outputs yet"}</h3>
                <p>{filtered ? "Try fewer filters or a different search." : rows.length ? "Switch to All to include finished goals." : "They appear here and on the tree as agents make them."}</p>
                {filtered ? <button className="btn btn-sm" onClick={() => reset()}>Clear all filters</button>
                  : rows.length > 0 && fm.scope !== "all" && <button className="btn btn-sm" onClick={() => set({ scope: "all" })}>Show all goals</button>}
              </div>
            ) : groups.map((g) => (
              <div className="fm-grp" key={g.key || "all"}>
                {fm.group !== "none" && (
                  <h3>{g.label}<span className="n">{g.rows.length}</span>
                    {fm.group === "task" && g.key && <button className="linkbtn" onClick={() => showOnTree(g.rows[0])}>Show on tree</button>}
                  </h3>
                )}
                {wrap(g.rows)}
              </div>
            ))}
          </section>
          {preview}
        </div>
        {panel && (
          <div className="fm-panel" role="group" aria-label="Filters">
            <div className="row" style={{ marginBottom: 8 }}><b>Filters</b><span className="grow" /><button className="linkbtn" onClick={() => reset()}>Clear all</button><button className="icon-btn" onClick={() => setPanel(false)} aria-label="Close filters">✕</button></div>
            <div className="fm-grid">
              <div className="fg"><h6>Goal</h6><div className="scroll">{check("goals", scopeGoals.map((g) => [g.id, g.title]))}</div></div>
              <div className="fg"><h6>Task</h6><div className="scroll">{check("tasks", scopeTasks.map((t) => [t.id, t.title]))}</div></div>
              <div className="fg"><h6>Staff</h6>{check("staff", staff.map((id) => [id, staffName(id)]))}</div>
              <div className="fg"><h6>Type</h6>{check("types", TYPES.map((t) => [t, typeLabel(t)]))}</div>
              <div className="fg"><h6>Status</h6>{check("statuses", Object.entries(STATUS_LABEL))}</div>
              {models.length > 0 && <div className="fg"><h6>Model</h6>{check("models", models.map((m) => [m, m]))}</div>}
              <div className="fg"><h6>File size</h6>{check("sizes", Object.entries(SIZE_LABEL))}</div>
              <div className="fg"><h6>Created</h6>{range("created")}</div>
              <div className="fg"><h6>Updated</h6>{range("updated")}</div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
