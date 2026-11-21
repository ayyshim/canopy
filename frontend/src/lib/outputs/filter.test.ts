import assert from "node:assert/strict";
import { test } from "node:test";
import { applyFm, clearFilters, counts, defaultFm, fromSearch, groupRows, isFiltered, stripSearch, toSearch, type Row } from "./filter.ts";

const row = (id: string, o: Partial<Row> = {}): Row => ({
  id, title: id, name: id + ".md", type: "document", status: "", isNew: false, taskId: "t1", taskTitle: "Build it", goalId: "g1", goalTitle: "Ship",
  goalActive: true, staffId: "m1", staffName: "Tommy", model: "Opus", size: 1000, versions: 1, notes: "", created: "2026-11-10T10:00:00Z", updated: "2026-11-10T10:00:00Z", ...o,
});
const rows = [
  row("a", { title: "Wireframe", type: "image", status: "review", updated: "2026-11-20T10:00:00Z" }),
  row("b", { title: "Report", type: "report", isNew: true, staffId: "m2", staffName: "Sam", taskId: "t2", taskTitle: "Test it", updated: "2026-11-18T10:00:00Z", size: 3_000_000 }),
  row("c", { title: "Old plan", goalId: "g2", goalTitle: "Done goal", goalActive: false, updated: "2026-10-01T10:00:00Z" }),
  row("d", { title: "Loose", goalId: "", goalTitle: "", taskId: "", taskTitle: "", goalActive: false }),
];
const ids = (r: Row[]) => r.map((x) => x.id);

test("active scope hides finished goals and outputs without a goal", () => {
  assert.deepEqual(ids(applyFm(rows, defaultFm())), ["a", "b"]);
  assert.deepEqual(ids(applyFm(rows, { ...defaultFm(), scope: "finished" })), ["c"]);
  assert.deepEqual(ids(applyFm(rows, { ...defaultFm(), scope: "all" })).sort(), ["a", "b", "c", "d"]);
});

test("search needs every word and looks at task, staff and notes", () => {
  const f = { ...defaultFm(), scope: "all" as const };
  assert.deepEqual(ids(applyFm(rows, { ...f, q: "sam test" })), ["b"]);
  assert.deepEqual(ids(applyFm(rows, { ...f, q: "WIREFRAME image" })), ["a"]);
  assert.deepEqual(ids(applyFm(rows, { ...f, q: "nothing here" })), []);
});

test("filters combine, and status uses new only when nothing else applies", () => {
  const f = defaultFm();
  assert.deepEqual(ids(applyFm(rows, { ...f, statuses: ["review"] })), ["a"]);
  assert.deepEqual(ids(applyFm(rows, { ...f, statuses: ["new"] })), ["b"]);
  assert.deepEqual(ids(applyFm(rows, { ...f, staff: ["m2"], types: ["report"] })), ["b"]);
  assert.deepEqual(ids(applyFm(rows, { ...f, sizes: ["l"] })), ["b"]);
});

test("date ranges include both end days", () => {
  const f = { ...defaultFm(), updated: { from: "2026-11-18", to: "2026-11-20" } };
  assert.deepEqual(ids(applyFm(rows, f)), ["a", "b"]);
  assert.deepEqual(ids(applyFm(rows, { ...f, updated: { from: "2026-11-19", to: "" } })), ["a"]);
});

test("sorting by updated, title and size", () => {
  assert.deepEqual(ids(applyFm(rows, { ...defaultFm(), sort: "updated-asc" })), ["b", "a"]);
  assert.deepEqual(ids(applyFm(rows, { ...defaultFm(), sort: "title-asc" })), ["b", "a"]);
  assert.deepEqual(ids(applyFm(rows, { ...defaultFm(), sort: "size-desc" })), ["b", "a"]);
});

test("grouping keeps order and falls back to a plain label", () => {
  const g = groupRows(applyFm(rows, { ...defaultFm(), scope: "all" }), "task");
  assert.deepEqual(g.map((x) => [x.label, x.rows.length]), [["Build it", 2], ["Test it", 1], ["No task", 1]]);
  assert.equal(groupRows(rows, "none").length, 1);
});

test("counts ignore the field's own filter", () => {
  const f = { ...defaultFm(), staff: ["m1"] };
  assert.equal(counts(rows, f, "staff").get("m2"), 1);
  assert.equal(counts(rows, f, "types").get("report"), undefined);
});

test("state survives the URL, and only differences are written", () => {
  const f = { ...defaultFm(), q: "wire frame", goals: ["g1"], staff: ["m1", "m2"], sizes: ["l" as const], scope: "all" as const, group: "task" as const, view: "list" as const, created: { from: "2026-11-01", to: "" } };
  assert.deepEqual(fromSearch(toSearch(f)), f);
  assert.equal(toSearch(defaultFm()), "outputs=1");
  assert.equal(fromSearch("?goal=abc"), null);
});

test("bad URL values are ignored and other params are kept on strip", () => {
  const f = fromSearch("outputs=1&oscope=weird&ogroup=x&ocf=yesterday&oz=s,q");
  assert.deepEqual(f, { ...defaultFm(), sizes: ["s"] });
  assert.equal(stripSearch("outputs=1&oq=a&goal=xyz"), "goal=xyz");
});

test("clear keeps layout choices", () => {
  const f = clearFilters({ ...defaultFm(), q: "x", types: ["image"], group: "none", view: "list" });
  assert.equal(isFiltered(f), false);
  assert.equal(f.group, "none");
  assert.equal(f.view, "list");
});
