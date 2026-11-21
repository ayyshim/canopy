import assert from "node:assert/strict";
import { test } from "node:test";
import { clipboardName, MAX_FILES, pickFiles, pickMessage } from "./files.ts";

const MB = 1024 * 1024;
const f = (name: string, size = 10) => ({ name, size });

test("files over the limit are named and left out", () => {
  const r = pickFiles([f("a.txt"), f("big.zip", 26 * MB), f("edge.bin", 25 * MB), f("huge.mov", 90 * MB)]);
  assert.deepEqual(r.accepted.map((x) => x.name), ["a.txt", "edge.bin"]);
  assert.deepEqual(r.tooBig, ["big.zip", "huge.mov"]);
  assert.equal(pickMessage(r), "big.zip, huge.mov are over 25 MB and wasn’t added.");
});

test("one big file reads in the singular", () => {
  assert.equal(pickMessage(pickFiles([f("x.zip", 30 * MB)])), "x.zip is over 25 MB and wasn’t added.");
});

test("too many files at once are cut off", () => {
  const many = Array.from({ length: MAX_FILES + 3 }, (_, i) => f(`f${i}`));
  const r = pickFiles(many);
  assert.equal(r.accepted.length, MAX_FILES);
  assert.equal(r.extra, 3);
  assert.match(pickMessage(r), /3 left out/);
});

test("files already held count against the limit", () => {
  const r = pickFiles([f("a"), f("b"), f("c")], MAX_FILES - 1);
  assert.equal(r.accepted.length, 1);
  assert.equal(r.extra, 2);
});

test("nothing wrong gives no message", () => assert.equal(pickMessage(pickFiles([f("a")])), ""));

test("pasted screenshots get distinct names, real names are kept", () => {
  const at = new Date(2026, 10, 21, 9, 5, 7);
  assert.equal(clipboardName({ name: "image.png", type: "image/png" }, 0, at), "screenshot-20261121-090507.png");
  assert.equal(clipboardName({ name: "image.png", type: "image/png" }, 1, at), "screenshot-20261121-090507-2.png");
  assert.equal(clipboardName({ name: "report.pdf", type: "application/pdf" }, 0, at), "report.pdf");
  assert.equal(clipboardName({ name: "", type: "image/jpeg" }, 0, at), "screenshot-20261121-090507.jpg");
});
