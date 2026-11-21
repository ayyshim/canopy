// Rules for files picked, dropped or pasted into an attachment field.
export const MAX_FILE_MB = 25;
export const MAX_FILES = 20;

export type Picked<T> = { accepted: T[]; tooBig: string[]; extra: number };

/** pickFiles keeps files within the size limit and the count limit (have = files already held). */
export function pickFiles<T extends { name: string; size: number }>(incoming: T[], have = 0, maxMB = MAX_FILE_MB, maxFiles = MAX_FILES): Picked<T> {
  const tooBig: string[] = [];
  const fit: T[] = [];
  for (const f of incoming) (f.size > maxMB * 1024 * 1024 ? tooBig.push(f.name) : fit.push(f));
  const room = Math.max(0, maxFiles - have);
  return { accepted: fit.slice(0, room), tooBig, extra: Math.max(0, fit.length - room) };
}

/** pickMessage says what was left out, or "" when everything was taken. */
export function pickMessage(r: Picked<unknown>, maxMB = MAX_FILE_MB, maxFiles = MAX_FILES): string {
  const out: string[] = [];
  if (r.tooBig.length) out.push(`${r.tooBig.join(", ")} ${r.tooBig.length > 1 ? "are" : "is"} over ${maxMB} MB and wasn’t added.`);
  if (r.extra) out.push(`Up to ${maxFiles} files at a time; ${r.extra} left out.`);
  return out.join(" ");
}

const EXT: Record<string, string> = { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp" };

/** Pasted screenshots arrive as "image.png"; give each a name that tells them apart. */
export function clipboardName(f: { name: string; type: string }, index: number, at: Date): string {
  if (f.name && !/^image\.\w+$/i.test(f.name)) return f.name;
  const p = (n: number) => String(n).padStart(2, "0");
  const stamp = `${at.getFullYear()}${p(at.getMonth() + 1)}${p(at.getDate())}-${p(at.getHours())}${p(at.getMinutes())}${p(at.getSeconds())}`;
  return `screenshot-${stamp}${index ? "-" + (index + 1) : ""}.${EXT[f.type] ?? (f.type.split("/")[1] || "bin")}`;
}
