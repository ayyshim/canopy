"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { findOutputByName, pageName } from "@/lib/prototype";
import type { OutputView } from "@/lib/types";

const SANDBOX = "allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads";
const WIDTHS = { desktop: "100%", tablet: "768px", phone: "390px" } as const;
type Width = keyof typeof WIDTHS;
type Grant = { base: string; expires: number };

const enc = (name: string) => name.split("/").map(encodeURIComponent).join("/");

/** usePreviewGrant asks the hub for a preview link, and again before it expires or when the output changes. */
function useGrant(id: string, rev: number, reload: number) {
  const [got, setGot] = useState<{ key: string; grant: Grant | null } | null>(null);
  const [renew, setRenew] = useState(0);
  const key = `${id}:${rev}:${reload}:${renew}`;
  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    api<{ url: string; expires_at: string }>("POST", `/outputs/${id}/preview`).then(
      (r) => {
        if (!live) return;
        const base = /^(\/api\/v1\/preview\/[^/]+\/)/.exec(r.url)?.[1];
        const expires = Date.parse(r.expires_at);
        setGot({ key, grant: base ? { base, expires } : null });
        if (base) timer = setTimeout(() => setRenew((x) => x + 1), Math.max(30_000, expires - Date.now() - 120_000));
      },
      () => live && setGot({ key, grant: null }),
    );
    return () => { live = false; clearTimeout(timer); };
  }, [id, key]);
  return got && got.key === key ? got.grant ?? "failed" : got && got.key.startsWith(`${id}:`) ? got.grant ?? "failed" : null;
}

/** Prototype shows an HTML output as a live page. Links between pages move the side panel, not the page. */
export function Prototype({ o, outputs, goalOf, rev, header, onOpen }: {
  o: OutputView;
  outputs: OutputView[];
  goalOf: (taskId: string) => string | undefined;
  rev: number;
  header: React.ReactNode;
  onOpen: (id: string) => void;
}) {
  const stage = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const gotMessage = useRef(false);
  const [via, setVia] = useState<string | null>(null);

  const [entry, setEntry] = useState(o);
  const [seen, setSeen] = useState(o.id);
  const [page, setPage] = useState(o.name);
  const [trail, setTrail] = useState([o.name]);
  const [nav, setNav] = useState(0);
  const [missing, setMissing] = useState("");
  const [width, setWidth] = useState<Width>("desktop");
  const [fs, setFs] = useState<"" | "native" | "overlay">("");

  // Opening another output (not a link inside the page) starts a new prototype.
  if (seen !== o.id) {
    setSeen(o.id);
    if (via !== o.id) {
      setEntry(o); setPage(o.name); setTrail([o.name]); setMissing(""); setNav((x) => x + 1);
    }
    setVia(null);
  }

  const grant = useGrant(entry.id, rev, nav);
  const ready = grant && grant !== "failed" ? grant : null;
  const src = ready ? ready.base + enc(page) : "";

  const go = useCallback((name: string) => {
    const target = findOutputByName(outputs, entry, name, goalOf);
    setPage(name); setMissing(""); setNav((x) => x + 1);
    if (target) { setVia(target.id); onOpen(target.id); }
  }, [outputs, entry, goalOf, onOpen]);

  useEffect(() => {
    const onMsg = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow || e.data?.type !== "canopy-preview" || typeof e.data.name !== "string") return;
      gotMessage.current = true;
      const name = pageName(e.data.name);
      setPage(name);
      setTrail((t) => (t[t.length - 1] === name ? t : [...t, name]));
      const target = findOutputByName(outputs, entry, name, goalOf);
      if (!target) { setMissing(name); return; }
      setMissing("");
      if (target.id !== o.id) { setVia(target.id); onOpen(target.id); }
    };
    window.addEventListener("message", onMsg);
    return () => window.removeEventListener("message", onMsg);
  }, [outputs, entry, goalOf, o.id, onOpen]);

  // A page the hub can't find has no script, so it never reports in.
  const onLoad = () => {
    gotMessage.current = false;
    const shown = page;
    setTimeout(() => { if (!gotMessage.current && frame.current) setMissing(shown); }, 1500);
  };

  const back = () => {
    if (trail.length < 2) return;
    const t = trail.slice(0, -1);
    setTrail(t);
    go(t[t.length - 1]);
  };
  const home = () => { setTrail([entry.name]); go(entry.name); };

  const toggleFs = useCallback(async () => {
    const el = stage.current;
    if (!el) return;
    if (document.fullscreenElement === el) { await document.exitFullscreen(); return; }
    if (fs === "overlay") { setFs(""); return; }
    if (document.fullscreenEnabled && el.requestFullscreen) {
      try { await el.requestFullscreen(); setFs("native"); return; } catch { /* use the overlay */ }
    }
    setFs("overlay");
  }, [fs]);

  useEffect(() => {
    const onChange = () => { if (!document.fullscreenElement) setFs((f) => (f === "native" ? "" : f)); };
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).closest("textarea, input, select") || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "Escape" && fs === "overlay") { e.stopImmediatePropagation(); setFs(""); }
      else if (e.key === "f" || e.key === "F") { e.preventDefault(); void toggleFs(); }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [fs, toggleFs]);

  return (
    <div className={`stage${fs === "overlay" ? " fs-overlay" : ""}`} ref={stage}>
      {!fs && header}
      <div className="ptools" role="toolbar" aria-label="Prototype controls">
        <button onClick={back} disabled={trail.length < 2}>Back</button>
        <button onClick={home} disabled={page === entry.name && !missing}>Home</button>
        <button onClick={() => { setMissing(""); setNav((x) => x + 1); }}>Reload</button>
        <span className="pname" title={page}>{page}</span>
        <div className="seg dark" role="group" aria-label="Page width">
          {(Object.keys(WIDTHS) as Width[]).map((w) => (
            <button key={w} aria-pressed={width === w} onClick={() => setWidth(w)}>{w[0].toUpperCase() + w.slice(1)}</button>
          ))}
        </div>
        <button onClick={toggleFs} aria-pressed={!!fs} title="Full screen (F)">{fs ? "Exit full screen" : "Full screen"}</button>
      </div>
      <div className="pstage">
        <div className="pframe" style={{ maxWidth: WIDTHS[width] }}>
          {ready ? (
            <iframe key={`${nav}:${ready.base}:${rev}`} ref={frame} className="pif" title={o.title} src={src} sandbox={SANDBOX} onLoad={onLoad} />
          ) : grant === "failed" ? (
            <div className="pnote"><b>Couldn’t open this prototype</b><span>Try Reload in a moment.</span></div>
          ) : (
            <p className="hint" style={{ padding: 16 }}>Loading…</p>
          )}
          {missing && (
            <div className="pnote" role="alert">
              <b>Page not found</b>
              <span>“{missing}” isn’t one of this prototype’s files.</span>
              <div className="row"><button className="btn btn-sm" onClick={home}>Go home</button>{trail.length > 1 && <button className="btn btn-sm" onClick={back}>Go back</button>}</div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
