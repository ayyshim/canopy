import assert from "node:assert/strict";
import { test } from "node:test";
import { findOutputByName, isPrototype, pageName } from "./prototype.ts";

const html = [{ mime: "text/html; charset=utf-8" }];
const outs = [
  { id: "a1", name: "index.html", task_id: "tA", versions: html },
  { id: "a2", name: "assets/app.css", task_id: "tA", versions: [{ mime: "text/css" }] },
  { id: "a3", name: "shared.js", task_id: "tA", versions: [] },
  { id: "b1", name: "about.html", task_id: "tB", versions: html },
  { id: "b2", name: "shared.js", task_id: "tB", versions: [] },
  { id: "c1", name: "secret.html", task_id: "tC", versions: html },
];
const goalOf = (t: string) => ({ tA: "g1", tB: "g1", tC: "g2" })[t];
const find = (raw: string) => findOutputByName(outs, outs[0], raw, goalOf)?.id;

test("same task wins, then the rest of the goal", () => {
  assert.equal(find("shared.js"), "a3");
  assert.equal(find("about.html"), "b1");
});

test("subfolder names match", () => assert.equal(find("assets/app.css"), "a2"));

test("other goals and missing names do not match", () => {
  assert.equal(find("secret.html"), undefined);
  assert.equal(find("nope.html"), undefined);
  assert.equal(find(""), undefined);
});

test("query, hash and leading slash are ignored", () => {
  assert.equal(pageName("./about.html?x=1#top"), "about.html");
  assert.equal(find("/about.html#team"), "b1");
});

test("html outputs are prototypes", () => {
  assert.equal(isPrototype(outs[0]), true);
  assert.equal(isPrototype({ name: "page.HTM", versions: [] }), true);
  assert.equal(isPrototype(outs[1]), false);
});
