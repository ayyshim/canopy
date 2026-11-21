// Search, filter, sort and group for the outputs file manager. Pure, so it can be tested.
export type Row = {
  id: string;
  title: string;
  name: string;
  type: string; // image, recording, document, diff, report or prototype
  status: string; // review, approved, changes, or "" for none
  isNew: boolean;
  taskId: string;
  taskTitle: string;
  goalId: string;
  goalTitle: string;
  goalActive: boolean;
  staffId: string;
  staffName: string;
  model: string;
  size: number;
  versions: number;
  notes: string;
  created: string; // RFC3339
  updated: string;
};

export type Range = { from: string; to: string }; // YYYY-MM-DD, either may be empty
export type Scope = "active" | "finished" | "all";
export type Group = "goal" | "task" | "staff" | "type" | "none";
export type Sort = "updated-desc" | "updated-asc" | "created-desc" | "created-asc" | "title-asc" | "size-desc" | "versions-desc";
export type SizeBucket = "s" | "m" | "l";

export type Fm = {
  q: string;
  goals: string[];
  tasks: string[];
  staff: string[];
  types: string[];
  statuses: string[]; // review, approved, changes, new
  models: string[];
  sizes: SizeBucket[];
  created: Range;
  updated: Range;
  scope: Scope;
  group: Group;
  sort: Sort;
  view: "grid" | "list";
};

export const defaultFm = (): Fm => ({
  q: "", goals: [], tasks: [], staff: [], types: [], statuses: [], models: [], sizes: [],
  created: { from: "", to: "" }, updated: { from: "", to: "" },
  scope: "active", group: "goal", sort: "updated-desc", view: "grid",
});

export const SIZE_LABEL: Record<SizeBucket, string> = { s: "Under 100 KB", m: "100 KB to 2 MB", l: "Over 2 MB" };
export const sizeBucket = (b: number): SizeBucket => (b < 102400 ? "s" : b < 2097152 ? "m" : "l");

/** The status a person sees: "new" when there is nothing more important to say. */
export const statusOf = (r: Row) => r.status === "review" || r.status === "approved" || r.status === "changes" ? r.status : r.isNew ? "new" : "";

const startOf = (d: string) => new Date(d + "T00:00:00").getTime();
const endOf = (d: string) => new Date(d + "T23:59:59.999").getTime();
const inRange = (iso: string, r: Range) => {
  const t = new Date(iso).getTime();
  return !(r.from && t < startOf(r.from)) && !(r.to && t > endOf(r.to));
};

const inScope = (r: Row, s: Scope) => s === "all" || (s === "active" ? r.goalId !== "" && r.goalActive : r.goalId !== "" && !r.goalActive);

/** Which rows pass. `skip` leaves one filter out, so option counts can show what picking it would give. */
export function matches(r: Row, f: Fm, skip?: keyof Fm): boolean {
  if (skip !== "scope" && !inScope(r, f.scope)) return false;
  if (skip !== "goals" && f.goals.length && !f.goals.includes(r.goalId)) return false;
  if (skip !== "tasks" && f.tasks.length && !f.tasks.includes(r.taskId)) return false;
  if (skip !== "staff" && f.staff.length && !f.staff.includes(r.staffId)) return false;
  if (skip !== "types" && f.types.length && !f.types.includes(r.type)) return false;
  if (skip !== "statuses" && f.statuses.length && !f.statuses.includes(statusOf(r))) return false;
  if (skip !== "models" && f.models.length && !f.models.includes(r.model)) return false;
  if (skip !== "sizes" && f.sizes.length && !f.sizes.includes(sizeBucket(r.size))) return false;
  if (!inRange(r.created, f.created) || !inRange(r.updated, f.updated)) return false;
  const words = f.q.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length) {
    const hay = [r.title, r.name, r.taskTitle, r.goalTitle, r.staffName, r.type, r.notes].join(" ").toLowerCase();
    if (!words.every((w) => hay.includes(w))) return false;
  }
  return true;
}

const SORT: Record<Sort, (a: Row, b: Row) => number> = {
  "updated-desc": (a, b) => b.updated.localeCompare(a.updated),
  "updated-asc": (a, b) => a.updated.localeCompare(b.updated),
  "created-desc": (a, b) => b.created.localeCompare(a.created),
  "created-asc": (a, b) => a.created.localeCompare(b.created),
  "title-asc": (a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: "base" }),
  "size-desc": (a, b) => b.size - a.size,
  "versions-desc": (a, b) => b.versions - a.versions,
};

export function applyFm(rows: Row[], f: Fm): Row[] {
  return rows.filter((r) => matches(r, f)).sort((a, b) => SORT[f.sort](a, b) || a.title.localeCompare(b.title));
}

export type Bucket = { key: string; label: string; rows: Row[] };

/** Splits already sorted rows into groups, in the order each group first appears. */
export function groupRows(rows: Row[], g: Group, typeLabel: (t: string) => string = (t) => t): Bucket[] {
  if (g === "none") return [{ key: "", label: "", rows }];
  const out = new Map<string, Bucket>();
  for (const r of rows) {
    const [key, label] = g === "goal" ? [r.goalId, r.goalTitle || "No goal"] : g === "task" ? [r.taskId, r.taskTitle || "No task"]
      : g === "staff" ? [r.staffId, r.staffName] : [r.type, typeLabel(r.type)];
    const b = out.get(key) ?? { key, label, rows: [] };
    b.rows.push(r);
    out.set(key, b);
  }
  return [...out.values()];
}

/** How many rows each value of a field would give, ignoring that field's own filter. */
export function counts(rows: Row[], f: Fm, field: "goals" | "tasks" | "staff" | "types" | "statuses" | "models" | "sizes"): Map<string, number> {
  const pick = { goals: (r: Row) => r.goalId, tasks: (r: Row) => r.taskId, staff: (r: Row) => r.staffId, types: (r: Row) => r.type, statuses: statusOf, models: (r: Row) => r.model, sizes: (r: Row) => sizeBucket(r.size) }[field];
  const m = new Map<string, number>();
  for (const r of rows) if (matches(r, f, field)) m.set(pick(r), (m.get(pick(r)) ?? 0) + 1);
  return m;
}

/** True when anything beyond the scope, grouping and layout narrows the list. */
export const isFiltered = (f: Fm) =>
  !!f.q.trim() || [f.goals, f.tasks, f.staff, f.types, f.statuses, f.models, f.sizes].some((a) => a.length) ||
  !!(f.created.from || f.created.to || f.updated.from || f.updated.to);

/** Clears the narrowing filters but keeps scope, grouping, sort and layout. */
export const clearFilters = (f: Fm): Fm => ({ ...defaultFm(), scope: f.scope, group: f.group, sort: f.sort, view: f.view });

/* ---------- URL ---------- */
// Param names start with "o" so they don't clash with ?goal= and ?task= from notification links.
const LISTS = { og: "goals", ot: "tasks", os: "staff", oy: "types", ost: "statuses", om: "models", oz: "sizes" } as const;
const DATE = /^\d{4}-\d{2}-\d{2}$/;
const GROUPS: Group[] = ["goal", "task", "staff", "type", "none"];
const SORTS = Object.keys(SORT) as Sort[];

/** The file manager state in a query string, only what differs from the defaults. "outputs=1" says it is open. */
export function toSearch(f: Fm): string {
  const d = defaultFm(), q = new URLSearchParams();
  q.set("outputs", "1");
  if (f.q.trim()) q.set("oq", f.q.trim());
  for (const [k, field] of Object.entries(LISTS)) { const v = f[field]; if (v.length) q.set(k, v.join(",")); }
  if (f.created.from) q.set("ocf", f.created.from);
  if (f.created.to) q.set("oct", f.created.to);
  if (f.updated.from) q.set("ouf", f.updated.from);
  if (f.updated.to) q.set("out", f.updated.to);
  if (f.scope !== d.scope) q.set("oscope", f.scope);
  if (f.group !== d.group) q.set("ogroup", f.group);
  if (f.sort !== d.sort) q.set("osort", f.sort);
  if (f.view !== d.view) q.set("oview", f.view);
  return q.toString();
}

/** The state in a query string, or null when the file manager is not open. Unknown values are ignored. */
export function fromSearch(search: string): Fm | null {
  const q = new URLSearchParams(search);
  if (q.get("outputs") !== "1") return null;
  const f = defaultFm();
  f.q = q.get("oq") ?? "";
  for (const [k, field] of Object.entries(LISTS)) {
    const v = (q.get(k) ?? "").split(",").filter(Boolean);
    if (field === "sizes") f.sizes = v.filter((x): x is SizeBucket => x === "s" || x === "m" || x === "l");
    else f[field] = v;
  }
  const date = (k: string) => { const v = q.get(k) ?? ""; return DATE.test(v) ? v : ""; };
  f.created = { from: date("ocf"), to: date("oct") };
  f.updated = { from: date("ouf"), to: date("out") };
  const scope = q.get("oscope");
  if (scope === "active" || scope === "finished" || scope === "all") f.scope = scope;
  const group = q.get("ogroup") as Group;
  if (GROUPS.includes(group)) f.group = group;
  const sort = q.get("osort") as Sort;
  if (SORTS.includes(sort)) f.sort = sort;
  if (q.get("oview") === "list") f.view = "list";
  return f;
}

/** Removes the file manager's params from a query string and leaves the rest alone. */
export function stripSearch(search: string): string {
  const q = new URLSearchParams(search);
  for (const k of ["outputs", "oq", "ocf", "oct", "ouf", "out", "oscope", "ogroup", "osort", "oview", ...Object.keys(LISTS)]) q.delete(k);
  return q.toString();
}
