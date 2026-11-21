// Pure geometry for the minimap, kept apart from React so it can be tested.
export type Box = { x0: number; y0: number; x1: number; y1: number };
export type Fit = { s: number; ox: number; oy: number };

/** Grow the tree's box a little so the view frame has room at the edges. */
export function mapBounds(box: Box, margin = 0.15): Box {
  const w = Math.max(box.x1 - box.x0, 1), h = Math.max(box.y1 - box.y0, 1);
  return { x0: box.x0 - w * margin, y0: box.y0 - h * margin, x1: box.x1 + w * margin, y1: box.y1 + h * margin };
}

/** Scale and offset that fit bounds inside a W×H map, centered. */
export function fitMap(b: Box, W: number, H: number): Fit {
  const s = Math.min(W / (b.x1 - b.x0), H / (b.y1 - b.y0));
  return { s, ox: (W - (b.x1 - b.x0) * s) / 2 - b.x0 * s, oy: (H - (b.y1 - b.y0) * s) / 2 - b.y0 * s };
}

/** A point on the map (px) as a point on the tree. */
export const mapToWorld = (f: Fit, mx: number, my: number) => ({ x: (mx - f.ox) / f.s, y: (my - f.oy) / f.s });

/** The camera that centers the board (w×h px) on a point of the tree. */
export const centerCam = (wx: number, wy: number, z: number, w: number, h: number) => ({ x: w / 2 - wx * z, y: h / 2 - wy * z });

/** The part of the tree the board shows. */
export const viewRect = (c: { x: number; y: number; z: number }, w: number, h: number) =>
  ({ x: -c.x / c.z, y: -c.y / c.z, w: w / c.z, h: h / c.z });

/** Shown unless the person chose otherwise; hidden by default on phones. */
export const minimapDefault = (stored: string | null, width: number) => (stored === "1" ? true : stored === "0" ? false : width >= 640);

/** How a node looks on the minimap: a fill, an optional border and an optional accent bar. Values are CSS tokens (--x) or colours. */
export type Look = { fill: string; edge?: string; accent?: string; side?: "left" | "top"; dashed?: boolean; fade?: boolean };

export type LookIn = {
  type: string;
  goalColor?: string; // goal node, task node, session node: the goal's own colour
  goalStatus?: string;
  taskStatus?: string;
  blocked?: boolean;
  mgr?: boolean;
  warn?: boolean; // agent needs a person
  sessionStatus?: string;
  outputStatus?: string;
  stickyColor?: string;
  sigKind?: string;
};

/** Mirrors the node styles in globals.css, so the map matches the board. */
export function nodeLook(n: LookIn): Look {
  const card = { fill: "--surface", edge: "--edge" };
  switch (n.type) {
    case "ws": return { fill: "--team" };
    case "goal":
      if (n.goalStatus === "ready") return { fill: "--surface", edge: "--amber", accent: n.goalColor, side: "left" };
      return { ...card, accent: n.goalColor, side: "left", dashed: n.goalStatus === "draft", fade: n.goalStatus === "done" || n.goalStatus === "stopped" };
    case "task": {
      const fill = n.taskStatus === "doing" || n.taskStatus === "review" ? "--amber-soft" : n.taskStatus === "done" ? "--green-soft" : "--surface";
      return { fill, edge: n.blocked ? "--red" : "--edge", accent: n.goalColor || "--edge", side: "left" };
    }
    case "member":
      if (n.mgr) return { ...card, edge: n.warn ? "--amber" : "--edge", accent: n.warn ? "--amber" : "--team", side: "top" };
      return { fill: "--surface", edge: n.warn ? "--amber" : "--edge" };
    case "pool": return { fill: "--soft", edge: "--teal" };
    case "session": return { fill: "--term", accent: n.goalColor || "--term-sub", side: "left", fade: n.sessionStatus === "asleep" };
    case "sticky": return { fill: `--n-${n.stickyColor ?? "yellow"}` };
    case "note": return { fill: "#DDF2E6" };
    case "fruit":
      if (n.outputStatus === "review") return { fill: "--surface", edge: "--amber" };
      if (n.outputStatus === "approved") return { fill: "--surface", edge: "--green" };
      return { fill: "--surface", edge: "--card" };
    case "more": return { fill: "--soft", edge: "--edge" };
    case "acct": case "dirs": return { fill: "--surface", edge: n.type === "acct" ? "--amber-edge" : "--edge" };
    case "sig": return { fill: "--surface", edge: n.sigKind === "errc" ? "--red" : "--amber-edge" };
    default: return card;
  }
}
