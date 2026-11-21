import assert from "node:assert/strict";
import { test } from "node:test";
import { findRefs, itemOf, parseRef, refOfHref, refLabel, remarkRefs, splitRefs } from "./refs.ts";

const labels = (s: string) => findRefs(s).map((r) => r.label);

test("refs are found with or without #, in any case", () => {
  assert.deepEqual(labels("See G-12, #t-34 and n-5."), ["G-12", "T-34", "N-5"]);
  assert.deepEqual(labels("T-1 and T-1 again"), ["T-1"]);
  assert.deepEqual(labels("(T-7) ends T-8."), ["T-7", "T-8"]);
});

test("refs glued to other words or paths are ignored", () => {
  assert.deepEqual(labels("ABT-34 and ref-12 and a/T-3 and T-4x"), []);
  assert.deepEqual(labels("G-0 is not an item"), []);
});

test("refs in code stay plain", () => {
  assert.deepEqual(labels("use `T-1` here, not T-2"), ["T-2"]);
  assert.deepEqual(labels("```\nT-3\n```\nT-4"), ["T-4"]);
  assert.deepEqual(labels("```\nunterminated T-5"), []);
});

test("splitRefs keeps every character", () => {
  const text = "Fix #T-34, then `N-1` and G-2.";
  const parts = splitRefs(text);
  assert.equal(parts.map((p) => (typeof p === "string" ? p : p.text)).join(""), text);
  assert.equal(parts.filter((p) => typeof p !== "string").length, 2);
});

test("parseRef reads one whole ref", () => {
  assert.deepEqual(parseRef("#g-9"), { kind: "goal", number: 9, label: "G-9" });
  assert.equal(parseRef("T-34 now"), null);
  assert.equal(parseRef("T-0"), null);
  assert.equal(refLabel("note", 5), "N-5");
});

test("only items the board shows resolve", () => {
  const b = { goals: [{ id: "g", number: 12 }], tasks: [{ id: "t", number: 34 }], notes: [] };
  assert.deepEqual(itemOf(b, parseRef("T-34")!), { kind: "task", id: "t" });
  assert.equal(itemOf(b, parseRef("N-5")!), undefined);
  assert.equal(itemOf(null, parseRef("T-34")!), undefined);
});

type N = { type: string; value?: string; url?: string; children?: N[] };
const run = (root: N, known: string[]) => { remarkRefs((r) => known.includes(r.label))()(root); return root; };

test("the Markdown plugin links known refs outside code and links", () => {
  const root: N = { type: "root", children: [{ type: "paragraph", children: [
    { type: "text", value: "Do T-34 and N-9, see " },
    { type: "inlineCode", value: "T-34" },
    { type: "link", url: "https://x.test", children: [{ type: "text", value: "T-34" }] },
  ] }] };
  run(root, ["T-34"]);
  const kids = root.children![0].children!;
  const links = kids.filter((k) => k.type === "link" && k.url?.startsWith("#ref:"));
  assert.equal(links.length, 1);
  assert.equal(links[0].url, "#ref:T-34");
  assert.equal(kids.find((k) => k.type === "inlineCode")?.value, "T-34");
  assert.equal(kids.filter((k) => k.type === "link").length, 2);
  assert.ok(kids.some((k) => k.type === "text" && k.value === "N-9"), "unknown refs stay text");
});

test("refOfHref only trusts #ref: links", () => {
  assert.equal(refOfHref("#ref:G-2")?.label, "G-2");
  assert.equal(refOfHref("https://x.test/#ref:G-2"), null);
  assert.equal(refOfHref(undefined), null);
});
