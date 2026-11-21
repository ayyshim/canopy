"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { camera } from "@/lib/board/camera";
import type { Layout } from "@/lib/board/layout";
import { useUI } from "@/lib/board/ui";
import { fitMap, mapBounds, mapToWorld, minimapDefault, viewRect, type Fit } from "@/lib/minimap";
import { useB } from "./context";

const W = 200, H = 130;
const KEY = "canopy-minimap";

/** The minimap's on/off state: remembered in localStorage, toggled with the M key. */
export function useMinimapOpen(): [boolean, () => void] {
  const [open, setOpen] = useState(() => {
    let stored: string | null = null;
    try { stored = localStorage.getItem(KEY); } catch { /* private window */ }
    return minimapDefault(stored, window.innerWidth);
  });
  const toggle = useCallback(() => {
    setOpen((v) => {
      try { localStorage.setItem(KEY, v ? "0" : "1"); } catch { /* private window */ }
      return !v;
    });
  }, []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).closest("input, textarea, select, [contenteditable]") || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "m" || e.key === "M") toggle();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toggle]);
  return [open, toggle];
}

type Rect = { x: number; y: number; w: number; h: number; c: string };
const TOKEN: Record<string, string> = { member: "--muted", session: "--blue", out: "--violet", ask: "--amber", err: "--red", plain: "--line" };

/** One small rect per node, in tree coordinates. Colors are tokens or a goal's own color. */
function rects(L: Layout, goalColor: Map<string, string>): Rect[] {
  const out: Rect[] = [];
  for (const n of L.nodes) {
    let c: string;
    switch (n.type) {
      case "addgoal": case "addm": continue;
      case "goal": c = n.g.color; break;
      case "task": c = (n.t.goal_id && goalColor.get(n.t.goal_id)) || TOKEN.plain; break;
      case "member": case "pool": c = TOKEN.member; break;
      case "session": c = TOKEN.session; break;
      case "fruit": case "more": c = TOKEN.out; break;
      case "acct": c = TOKEN.ask; break;
      case "sig": c = n.kind === "errc" ? TOKEN.err : TOKEN.ask; break;
      default: c = TOKEN.plain;
    }
    out.push({ x: n.x, y: n.y, w: n.w, h: n.h, c });
  }
  return out;
}

/** Minimap draws every node as a small rect and the current view as a frame. Click or drag to pan. */
export function Minimap({ L }: { L: Layout }) {
  const b = useB();
  const side = useUI((s) => s.side);
  const [clear, setClear] = useState(0); // px to move left so the details panel does not cover the map
  const ref = useRef<HTMLCanvasElement>(null);
  const goalColor = useMemo(() => new Map(b.goals.map((g) => [g.id, g.color])), [b.goals]);
  const items = useMemo(() => rects(L, goalColor), [L, goalColor]);
  const bounds = useMemo(() => mapBounds(L.box), [L.box]);
  useLayoutEffect(() => {
    const measure = () => {
      const board = ref.current?.closest(".board")?.getBoundingClientRect(), sb = document.querySelector(".sidebar")?.getBoundingClientRect();
      // On phones the panel is a sheet over the whole board; the map hides under it, which is fine.
      setClear(board && sb && side && sb.left > board.left + board.width / 2 ? Math.max(0, board.right - sb.left) : 0);
    };
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [side]);
  const fit = useRef<Fit>(fitMap(bounds, W, H));
  useEffect(() => { fit.current = fitMap(bounds, W, H); }, [bounds]);

  // Redraw from requestAnimationFrame, and only when the camera, the nodes or the theme changed.
  useEffect(() => {
    const cv = ref.current;
    const ctx = cv?.getContext("2d");
    if (!cv || !ctx) return;
    const dpr = window.devicePixelRatio || 1;
    cv.width = W * dpr;
    cv.height = H * dpr;
    let frame = 0, seen = -1, themeAt = -1e9, sig = "", style: CSSStyleDeclaration | null = null;
    const var_ = (c: string) => (c.startsWith("--") ? style!.getPropertyValue(c).trim() || "#888" : c);
    const draw = (ts: number) => {
      frame = requestAnimationFrame(draw);
      let dirty = camera.version() !== seen;
      if (ts - themeAt > 800) {
        themeAt = ts;
        style = getComputedStyle(document.documentElement);
        const s = style.getPropertyValue("--paper") + style.getPropertyValue("--blue");
        if (s !== sig) { sig = s; dirty = true; }
      }
      if (!dirty || !style) return;
      seen = camera.version();
      const f = fit.current, cam = camera.get();
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, W, H);
      for (const r of items) {
        ctx.fillStyle = var_(r.c);
        ctx.globalAlpha = 0.9;
        ctx.fillRect(r.x * f.s + f.ox, r.y * f.s + f.oy, Math.max(r.w * f.s, 2), Math.max(r.h * f.s, 2));
      }
      ctx.globalAlpha = 1;
      if (cam.w > 0) {
        const v = viewRect(cam, cam.w, cam.h);
        const x = v.x * f.s + f.ox, y = v.y * f.s + f.oy, w = v.w * f.s, h = v.h * f.s;
        ctx.fillStyle = var_("--ink");
        ctx.globalAlpha = 0.08;
        ctx.fillRect(x, y, w, h);
        ctx.globalAlpha = 1;
        ctx.strokeStyle = var_("--ink");
        ctx.lineWidth = 1.5;
        ctx.strokeRect(x + 0.75, y + 0.75, Math.max(w - 1.5, 1), Math.max(h - 1.5, 1));
      }
    };
    frame = requestAnimationFrame(draw);
    seen = -1;
    return () => cancelAnimationFrame(frame);
  }, [items]);

  const pan = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const p = mapToWorld(fit.current, ((e.clientX - r.left) / r.width) * W, ((e.clientY - r.top) / r.height) * H);
    camera.panTo(p.x, p.y);
  };
  return (
    <div className="minimap" role="group" aria-label="Board minimap" style={clear ? { right: 14 + clear } : undefined}>
      <canvas
        ref={ref}
        width={W}
        height={H}
        aria-label="Minimap. Click or drag to move the board view."
        onPointerDown={(e) => { e.currentTarget.setPointerCapture(e.pointerId); pan(e); }}
        onPointerMove={(e) => { if (e.currentTarget.hasPointerCapture(e.pointerId)) pan(e); }}
      />
    </div>
  );
}
