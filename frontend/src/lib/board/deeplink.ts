export type DeepLink = { goal?: string; task?: string; signal?: string };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Reads ?goal=&task=&signal= from a notification link; anything that isn't an id is ignored. */
export function parseDeepLink(search: string): DeepLink {
  const q = new URLSearchParams(search);
  const out: DeepLink = {};
  for (const k of ["goal", "task", "signal"] as const) {
    const v = q.get(k);
    if (v && UUID.test(v)) out[k] = v;
  }
  return out;
}

/** The workspace id in a /w/{id} path, if any. */
export function workspaceOf(pathname: string): string | null {
  const m = /^\/w\/([^/]+)/.exec(pathname);
  return m ? m[1] : null;
}
