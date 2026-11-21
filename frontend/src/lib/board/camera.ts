// The board camera, shared with anything that draws next to the canvas (the minimap). It is a
// plain store, not React state: the canvas publishes on every pan and zoom, readers poll it
// from requestAnimationFrame, so the board never re-renders because the camera moved.
type Snap = { x: number; y: number; z: number; w: number; h: number }; // w, h: the board's size in px

let snap: Snap = { x: 0, y: 0, z: 1, w: 0, h: 0 };
let version = 0;
let pan: ((wx: number, wy: number) => void) | null = null;

export const camera = {
  get: () => snap,
  /** Changes whenever the camera or the board's size does. */
  version: () => version,
  publish(c: { x: number; y: number; z: number }, w: number, h: number) {
    snap = { x: c.x, y: c.y, z: c.z, w, h };
    version++;
  },
  /** The canvas registers how to center the view on a point of the tree. */
  onPan(fn: (wx: number, wy: number) => void) {
    pan = fn;
    return () => { if (pan === fn) pan = null; };
  },
  panTo: (wx: number, wy: number) => pan?.(wx, wy),
};
