// Sticky notes: notes with a board position. Pure helpers, so they can be tested.
export const STICKY_COLORS = ["yellow", "green", "blue", "pink", "purple", "orange", "gray"] as const;
export type StickyColor = (typeof STICKY_COLORS)[number];
export const STICKY_W = 200;
export const STICKY_H = 150;

type NoteLike = {
  visibility: string;
  author_user_id?: string;
  board_x?: number | null;
  board_y?: number | null;
  edited_by_member_id?: string | null;
};

export const stickyColor = (c?: string): StickyColor => (STICKY_COLORS as readonly string[]).includes(c ?? "") ? (c as StickyColor) : "yellow";

/** A note is a sticky when it has a board position. */
export const isPinned = (n: NoteLike) => n.board_x != null && n.board_y != null;

export const isPrivate = (n: NoteLike) => n.visibility === "private";

/** The first lines of a note, as Markdown, for the face of a sticky. */
export function stickyPreview(body: string, maxLines = 5): string {
  const lines = body.replace(/\r/g, "").split("\n");
  while (lines.length && !lines[lines.length - 1].trim()) lines.pop();
  return lines.slice(0, maxLines).join("\n");
}

/** Authors change their notes; admins also change shared ones. Private notes stay with the author. */
export const canEditNote = (n: NoteLike, me: string, admin: boolean) =>
  n.author_user_id === me || (admin && !isPrivate(n));

/** Only the author changes who can see a note. */
export const canChangeVisibility = (n: NoteLike, me: string) => n.author_user_id === me;

/** Show "Edited by" only after an agent edited the note. */
export const editedByAgent = (n: NoteLike) => !!n.edited_by_member_id;

/** Where to put a new sticky so it is centered in the board's view. */
export function centerSpot(cam: { x: number; y: number; z: number }, w: number, h: number) {
  return { x: Math.round((w / 2 - cam.x) / cam.z - STICKY_W / 2), y: Math.round((h / 2 - cam.y) / cam.z - STICKY_H / 2) };
}

/** Moved by a drag of (dx, dy) screen pixels at zoom z. */
export const dragged = (x: number, y: number, dx: number, dy: number, z: number) => ({ x: Math.round(x + dx / z), y: Math.round(y + dy / z) });
