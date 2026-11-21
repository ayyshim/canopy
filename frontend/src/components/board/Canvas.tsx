"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { memberStatus } from "@/lib/board/derive";
import { camera } from "@/lib/board/camera";
import { layout, type Layout } from "@/lib/board/layout";
import { centerCam } from "@/lib/minimap";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { isHidden, type Board, type MemberView } from "@/lib/types";
import { useB, useBoardCtx } from "./context";
import { NodeView } from "./NodeView";
import { Minimap, useMinimapOpen } from "./Minimap";
import { StickyDraft } from "./Sticky";
import { STICKY_H, STICKY_W } from "@/lib/stickies";

type Cam = { x: number; y: number; z: number; auto: boolean };

/** Canvas is the pannable, zoomable tree with a living dot field behind it. */
export function Canvas() {
  const b = useB();
  const { send } = useBoardCtx();
  const side = useUI((s) => s.side);
  const draft = useUI((s) => s.sticky);
  const tray = useUI((s) => s.tray);
  const focus = useUI((s) => s.focus);
  const openSide = useUI((s) => s.openSide);
  const [heights, setHeights] = useState<Record<string, number>>({});
  const focusGoal = side?.type === "goal" ? side.id ?? null : null;
  const L = useMemo(() => layout(b, heights, focusGoal), [b, heights, focusGoal]);

  const boardRef = useRef<HTMLDivElement>(null);
  const worldRef = useRef<HTMLDivElement>(null);
  const nodesRef = useRef<HTMLDivElement>(null);
  const dotsRef = useRef<HTMLCanvasElement>(null);
  const cam = useRef<Cam>({ x: 40, y: 20, z: 0.8, auto: true });
  const [zoomPct, setZoomPct] = useState(80);
  const layoutRef = useRef<Layout>(L);
  useLayoutEffect(() => { layoutRef.current = L; }, [L]);

  const apply = useCallback(() => {
    const c = cam.current;
    if (worldRef.current) worldRef.current.style.transform = `translate(${c.x}px, ${c.y}px) scale(${c.z})`;
    setZoomPct(Math.round(c.z * 100));
    const r = boardRef.current;
    camera.publish(c, r?.clientWidth ?? 0, r?.clientHeight ?? 0);
  }, []);
  const [mapOpen, toggleMap] = useMinimapOpen();

  // The minimap asks to center the view on a point of the tree.
  useEffect(() => camera.onPan((wx, wy) => {
    const el = boardRef.current;
    if (!el) return;
    Object.assign(cam.current, centerCam(wx, wy, cam.current.z, el.clientWidth, el.clientHeight), { auto: false });
    apply();
  }), [apply]);

  const fit = useCallback((soft = false) => {
    const el = boardRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect(), bx = layoutRef.current.box, over = tray ? 300 : 0;
    const small = r.width < 640;
    // On phones keep text readable: never fit below 0.55 and start at the top left, then pan.
    const k = Math.max(small ? 0.55 : 0.3, Math.min(1.05, (r.width - 48) / (bx.x1 - bx.x0), (r.height - 90 - over) / (bx.y1 - bx.y0)));
    const w = (bx.x1 - bx.x0) * k;
    Object.assign(cam.current, { z: k, x: (w > r.width - 24 ? 16 : (r.width - w) / 2) - bx.x0 * k, y: 40 - bx.y0 * k });
    if (!soft) cam.current.auto = true;
    apply();
  }, [apply, tray]);

  const zoomAt = useCallback((f: number, cx: number, cy: number) => {
    const c = cam.current, nz = Math.max(0.25, Math.min(2.2, c.z * f));
    Object.assign(c, { x: cx - ((cx - c.x) * nz) / c.z, y: cy - ((cy - c.y) * nz) / c.z, z: nz, auto: false });
    apply();
  }, [apply]);

  // Measure signal cards (their height depends on their text) and lay out again if it changed.
  useLayoutEffect(() => {
    const host = nodesRef.current;
    if (!host) return;
    const next: Record<string, number> = {};
    let changed = false;
    host.querySelectorAll<HTMLElement>("[data-sig]").forEach((el) => {
      const id = el.dataset.sig!, h = el.offsetHeight;
      next[id] = h;
      if (h && Math.abs((heights[id] ?? 0) - h) > 1) changed = true;
    });
    // Measuring after layout is what layout effects are for; this settles after one pass.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (changed) setHeights((old) => ({ ...old, ...next }));
    if (cam.current.auto) fit(true);
  }, [L, heights, fit]);

  // A node that is not drawn yet (its goal's tasks are being shown) is centered once it appears.
  const focused = useRef(0);
  useEffect(() => {
    if (!focus || focused.current === focus.at) return;
    const n = L.byId[focus.id], el = boardRef.current;
    if (!n || !el) return;
    focused.current = focus.at;
    const r = el.getBoundingClientRect(), c = cam.current;
    c.auto = false;
    c.z = Math.max(c.z, 0.85);
    c.x = r.width / 2 - (n.x + n.w / 2) * c.z;
    c.y = r.height / 2.6 - (n.y + n.h / 2) * c.z;
    apply();
  }, [focus, L, apply]);

  // Pan by dragging, pinch to zoom, scroll to pan, Ctrl + scroll to zoom. Clicking empty space
  // closes the details panel.
  useEffect(() => {
    const el = boardRef.current;
    if (!el) return;
    const ptr = new Map<number, { x: number; y: number }>();
    let drag: { id: number; x: number; y: number; cx: number; cy: number; moved: boolean; onNode: boolean } | null = null;
    let pinch: { d: number; z: number; mx: number; my: number; cx: number; cy: number } | null = null;
    const skip = "button, input, select, textarea, a, .sticky, .zoomctl, .minimap, .sidebar, .chat, .tray, .hintcard, .banner, .menu";
    const down = (e: PointerEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest(skip) && !t.closest(".node")) return;
      ptr.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (ptr.size === 2) {
        const [p1, p2] = [...ptr.values()];
        pinch = { d: Math.hypot(p1.x - p2.x, p1.y - p2.y) || 1, z: cam.current.z, mx: (p1.x + p2.x) / 2, my: (p1.y + p2.y) / 2, cx: cam.current.x, cy: cam.current.y };
        drag = null;
        return;
      }
      drag = { id: e.pointerId, x: e.clientX, y: e.clientY, cx: cam.current.x, cy: cam.current.y, moved: false, onNode: !!t.closest(".node") };
    };
    const move = (e: PointerEvent) => {
      if (!ptr.has(e.pointerId)) return;
      ptr.set(e.pointerId, { x: e.clientX, y: e.clientY });
      const c = cam.current;
      if (pinch && ptr.size >= 2) {
        const [p1, p2] = [...ptr.values()], r = el.getBoundingClientRect();
        const z = Math.max(0.25, Math.min(2.2, (pinch.z * Math.hypot(p1.x - p2.x, p1.y - p2.y)) / pinch.d));
        const wx = (pinch.mx - r.left - pinch.cx) / pinch.z, wy = (pinch.my - r.top - pinch.cy) / pinch.z;
        Object.assign(c, { z, x: (p1.x + p2.x) / 2 - r.left - wx * z, y: (p1.y + p2.y) / 2 - r.top - wy * z, auto: false });
        apply();
        return;
      }
      if (!drag) return;
      const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
      if (!drag.moved && Math.hypot(dx, dy) > (e.pointerType === "touch" ? 8 : 4)) {
        drag.moved = true;
        el.classList.add("panning");
        try { el.setPointerCapture(drag.id); } catch { /* the pointer may be gone */ }
      }
      if (drag.moved) {
        Object.assign(c, { x: drag.cx + dx, y: drag.cy + dy, auto: false });
        apply();
      }
    };
    const up = (e: PointerEvent) => {
      ptr.delete(e.pointerId);
      if (pinch) {
        if (ptr.size < 2) pinch = null;
        return;
      }
      if (!drag) return;
      const d = drag;
      drag = null;
      el.classList.remove("panning");
      if (d.moved) {
        // Swallow the click that ends a drag.
        const stop = (ev: MouseEvent) => { ev.stopPropagation(); ev.preventDefault(); };
        window.addEventListener("click", stop, { capture: true, once: true });
        setTimeout(() => window.removeEventListener("click", stop, { capture: true }), 50);
      } else if (!d.onNode && !(e.target as HTMLElement).closest(skip)) {
        openSide(null);
      }
    };
    const wheel = (e: WheelEvent) => {
      if ((e.target as HTMLElement).closest(".sidebar, .chat, .tray, .menu, .hintcard, .toasts")) return;
      e.preventDefault();
      const r = el.getBoundingClientRect(), c = cam.current;
      if (e.ctrlKey || e.metaKey) zoomAt(Math.exp(-e.deltaY * 0.01), e.clientX - r.left, e.clientY - r.top);
      else if (e.deltaMode === 0 && (Math.abs(e.deltaX) > 0 || Math.abs(e.deltaY) < 40)) {
        Object.assign(c, { x: c.x - e.deltaX, y: c.y - e.deltaY, auto: false });
        apply();
      } else zoomAt(Math.exp(-e.deltaY * 0.0015 * (e.deltaMode ? 30 : 1)), e.clientX - r.left, e.clientY - r.top);
    };
    // Share where this viewer's cursor is, in tree coordinates.
    let lastSent = 0;
    const cursor = (e: PointerEvent) => {
      if (performance.now() - lastSent < 70) return;
      lastSent = performance.now();
      const r = el.getBoundingClientRect(), c = cam.current;
      const over = (e.target as HTMLElement).closest<HTMLElement>("[data-member]")?.dataset.member ?? null;
      send("presence.cursor", { x: (e.clientX - r.left - c.x) / c.z, y: (e.clientY - r.top - c.y) / c.z, over_member_id: over });
    };
    // Double-click on empty canvas starts a sticky note at that spot.
    const dbl = (e: MouseEvent) => {
      if ((e.target as HTMLElement).closest(`${skip}, .nw, .cursor`)) return;
      const r = el.getBoundingClientRect(), c = cam.current;
      useUI.getState().startSticky({ x: Math.round((e.clientX - r.left - c.x) / c.z - STICKY_W / 2), y: Math.round((e.clientY - r.top - c.y) / c.z - STICKY_H / 2) });
    };
    el.addEventListener("dblclick", dbl);
    el.addEventListener("pointerdown", down);
    el.addEventListener("pointermove", move);
    el.addEventListener("pointermove", cursor);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
    el.addEventListener("wheel", wheel, { passive: false });
    const resize = () => cam.current.auto && fit(true);
    window.addEventListener("resize", resize);
    return () => {
      el.removeEventListener("dblclick", dbl);
      el.removeEventListener("pointerdown", down);
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointermove", cursor);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      el.removeEventListener("wheel", wheel);
      window.removeEventListener("resize", resize);
    };
  }, [apply, fit, zoomAt, openSide, send]);

  useDotField(dotsRef, boardRef, cam, L, b);

  const bx = L.box;
  return (
    <div className="board" id="board" ref={boardRef}>
      <canvas id="dots" ref={dotsRef} aria-hidden="true" />
      <div className="world" ref={worldRef}>
        <svg className="edges" width={bx.x1 + 400} height={bx.y1 + 120} style={{ left: 0, top: 0 }}>
          <defs>
            <marker id="ar-vi" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" style={{ fill: "var(--violet)" }} /></marker>
            <marker id="ar-red" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" style={{ fill: "var(--red)" }} /></marker>
          </defs>
          <g>
            {L.edges.map((e, i) => (
              <path key={i} className={e.cls} d={e.d} stroke={e.stroke} strokeWidth={e.stroke ? 1.6 : undefined} fill={e.stroke ? "none" : undefined}
                opacity={e.opacity} markerEnd={e.marker ? `url(#ar-${e.marker})` : undefined} />
            ))}
          </g>
        </svg>
        <div ref={nodesRef}>
          {L.nodes.map((n) => (
            <div className="nw" key={n.id} data-member={n.type === "member" ? n.memberId : undefined}><NodeView n={n} /></div>
          ))}
        </div>
        {draft && <StickyDraft at={draft} />}
        <Cursors />
      </div>
      <div className="zoomctl">
        <button onClick={() => { const r = boardRef.current!.getBoundingClientRect(); zoomAt(1 / 1.2, r.width / 2, r.height / 2); }} aria-label="Zoom out">−</button>
        <button className="zl tnum" onClick={() => fit()} title="Fit everything">{zoomPct}%</button>
        <button onClick={() => { const r = boardRef.current!.getBoundingClientRect(); zoomAt(1.2, r.width / 2, r.height / 2); }} aria-label="Zoom in">+</button>
        <button className="mapbtn" onClick={toggleMap} aria-pressed={mapOpen} aria-label={mapOpen ? "Hide minimap" : "Show minimap"} title="Minimap (M)">
          <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><rect x="2" y="3" width="14" height="12" rx="2" /><rect x="8" y="7" width="5" height="4" rx="1" /></svg>
        </button>
      </div>
      {mapOpen && <Minimap L={L} />}
      <HintCard />
    </div>
  );
}

/** Other people's cursors on the tree. */
function Cursors() {
  const cursors = useBoard((s) => s.cursors);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(id);
  }, []);
  return (
    <>
      {Object.values(cursors).filter((c) => now - c.at < 15000).map((c) => (
        <div className="cursor" key={c.user_id} style={{ transform: `translate(${c.x}px, ${c.y}px)` }}>
          <svg width="16" height="18" viewBox="0 0 16 18"><path d="M1 1l13 7-6 1.5L5.5 16z" fill={c.avatar_color} stroke="#fff" strokeWidth="1.2" /></svg>
          <span style={{ background: c.avatar_color }}>{c.name}</span>
        </div>
      ))}
    </>
  );
}

function HintCard() {
  const [gone, setGone] = useState(() => {
    try { return localStorage.getItem("canopy-hint") === "x"; } catch { return false; }
  });
  if (gone) return null;
  return (
    <div className="hintcard">
      <span>Give goals to managers. They plan the work and hand tasks to their staff, borrowing independent staff when they need help. Questions, approvals and outputs show up right on the tree.</span>
      <button className="btn btn-sm" onClick={() => { setGone(true); try { localStorage.setItem("canopy-hint", "x"); } catch { /* private window */ } }}>Got it</button>
    </div>
  );
}

// The living dot field: dots near a card take its state color and pulse (blue working,
// amber needs a person, red error).
const DCOL: Record<string, number[]> = { b: [62, 123, 250], y: [226, 170, 20], r: [229, 72, 77] };

function hexRGB(h: string): number[] {
  h = h.trim().replace("#", "");
  if (h.length === 3) h = h.split("").map((c) => c + c).join("");
  const n = parseInt(h, 16);
  return isNaN(n) ? [205, 212, 221] : [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function aura(b: Board, L: Layout) {
  const out: { x: number; y: number; w: number; h: number; c: string }[] = [];
  const put = (id: string | undefined | null, c: string) => {
    const p = id ? L.byId[id] : undefined;
    if (p) out.push({ x: p.x, y: p.y, w: p.w, h: p.h, c });
  };
  for (const raw of b.members) {
    if (isHidden(raw)) continue;
    const m = raw as MemberView;
    const k = memberStatus(b, m).k, c = k === "error" ? "r" : k === "need" ? "y" : k === "working" ? "b" : null;
    if (!c) continue;
    put(m.id, c);
    if (c === "b") put(b.tasks.find((x) => x.member_id === m.id && x.status === "doing")?.id, "b");
  }
  for (const s of b.signals) {
    if (s.type === "usage_limit" || s.type === "cap") { put("acct_" + s.id, "y"); continue; }
    const c = s.type === "error" ? "r" : "y";
    put(s.id, c);
    put(s.task_id, c);
  }
  for (const g of b.goals) if (g.status !== "done" && b.tasks.some((t) => t.goal_id === g.id && t.status === "doing")) put(g.id, "b");
  return out;
}

function useDotField(dots: React.RefObject<HTMLCanvasElement | null>, board: React.RefObject<HTMLDivElement | null>, cam: React.RefObject<Cam>, L: Layout, b: Board) {
  const auraRef = useRef<ReturnType<typeof aura>>([]);
  const spots = useMemo(() => aura(b, L), [b, L]);
  useEffect(() => { auraRef.current = spots; }, [spots]);
  useEffect(() => {
    let frame = 0, themeAt = -1e9, base = [205, 212, 221];
    const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const draw = (ts: number) => {
      frame = requestAnimationFrame(draw);
      const c = dots.current, el = board.current, cm = cam.current;
      if (!c || !el || !cm) return;
      const ctx = c.getContext("2d");
      if (!ctx) return;
      const r = el.getBoundingClientRect(), dpr = window.devicePixelRatio || 1;
      if (c.width !== Math.round(r.width * dpr) || c.height !== Math.round(r.height * dpr)) {
        c.width = Math.round(r.width * dpr);
        c.height = Math.round(r.height * dpr);
      }
      if (ts - themeAt > 800) {
        base = hexRGB(getComputedStyle(document.documentElement).getPropertyValue("--dot"));
        themeAt = ts;
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, r.width, r.height);
      let sp = 22 * cm.z;
      while (sp < 10) sp *= 2;
      const ox = ((cm.x % sp) + sp) % sp, oy = ((cm.y % sp) + sp) % sp, t = ts / 1000, R = 72 * cm.z;
      const src = auraRef.current
        .map((a) => ({ x0: a.x * cm.z + cm.x, y0: a.y * cm.z + cm.y, x1: (a.x + a.w) * cm.z + cm.x, y1: (a.y + a.h) * cm.z + cm.y, c: a.c }))
        .filter((a) => a.x1 > -R && a.x0 < r.width + R && a.y1 > -R && a.y0 < r.height + R);
      ctx.fillStyle = `rgb(${base[0]},${base[1]},${base[2]})`;
      ctx.beginPath();
      const hot: (number | string)[] = [];
      for (let y = oy; y < r.height; y += sp) {
        for (let x = ox; x < r.width; x += sp) {
          let best = 0, bc = "", bd = 0;
          for (const a of src) {
            const dx = Math.max(a.x0 - x, 0, x - a.x1), dy = Math.max(a.y0 - y, 0, y - a.y1);
            if (dx > R || dy > R) continue;
            const d = Math.hypot(dx, dy);
            if (d < R && 1 - d / R > best) { best = 1 - d / R; bc = a.c; bd = d; }
          }
          if (best > 0) hot.push(x, y, best, bd, bc);
          else { ctx.moveTo(x + 1.1, y); ctx.arc(x, y, 1.1, 0, 6.2832); }
        }
      }
      ctx.fill();
      for (let i = 0; i < hot.length; i += 5) {
        const x = hot[i] as number, y = hot[i + 1] as number, inf = hot[i + 2] as number, d = (hot[i + 3] as number) / Math.max(cm.z, 0.3), c = hot[i + 4] as string, col = DCOL[c];
        let w = 0.6;
        if (!still) w = c === "b" ? 0.5 + 0.5 * Math.sin(t * 1.5 - d / 18) : c === "y" ? 0.5 + 0.5 * Math.sin(t * 1.1) : 0.5 + 0.5 * Math.sin(t * 2.6 - d / 16);
        const a = inf * inf * (0.5 + 0.5 * w), rad = 1.1 + 0.8 * a, mix = Math.min(0.7, 0.12 + a * 0.65);
        ctx.fillStyle = `rgba(${Math.round(base[0] + (col[0] - base[0]) * mix)},${Math.round(base[1] + (col[1] - base[1]) * mix)},${Math.round(base[2] + (col[2] - base[2]) * mix)},${0.55 + 0.45 * mix})`;
        ctx.beginPath();
        ctx.arc(x, y, rad, 0, 6.2832);
        ctx.fill();
      }
    };
    frame = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(frame);
  }, [dots, board, cam]);
}
