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
