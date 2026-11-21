import assert from "node:assert/strict";
import { test } from "node:test";
import { isExternal, siblingName } from "./outputPath.ts";
import { isPrototype } from "./prototype.ts";

test("paths resolve from the folder of the current output", () => {
  assert.equal(siblingName("report.md", "shots/home.png"), "shots/home.png");
  assert.equal(siblingName("report.md", "./shots/home.png"), "shots/home.png");
  assert.equal(siblingName("docs/report.md", "shots/home.png"), "docs/shots/home.png");
  assert.equal(siblingName("docs/report.md", "../index.html"), "index.html");
  assert.equal(siblingName("docs/report.md", "/index.html"), "index.html");
  assert.equal(siblingName("report.md", "a/./b//c.png"), "a/b/c.png");
});

test("query, hash and encoding are handled", () => {
  assert.equal(siblingName("report.md", "page.html#top"), "page.html");
  assert.equal(siblingName("report.md", "page.html?x=1"), "page.html");
  assert.equal(siblingName("report.md", "my%20shot.png"), "my shot.png");
  assert.equal(siblingName("report.md", "bad%zz.png"), "bad%zz.png");
});

test("external links, anchors and escapes are not siblings", () => {
  for (const raw of ["https://example.com/a.png", "mailto:a@b.co", "//cdn.test/x.js", "data:image/png;base64,AAAA", "javascript:alert(1)", "#section", "", "  "]) {
    assert.equal(siblingName("report.md", raw), null, raw);
  }
  assert.equal(siblingName("report.md", "../secret.png"), null);
  assert.equal(siblingName("a/report.md", "../../x.png"), null);
  assert.equal(siblingName("report.md", "."), null);
  assert.equal(isExternal("HTTPS://x.test"), true);
  assert.equal(isExternal("shots/home.png"), false);
});

test("HTML outputs are prototypes by name or by type, with or without an extension", () => {
  const v = (mime: string) => [{ mime }];
  assert.equal(isPrototype({ name: "index.html", versions: v("text/plain") }), true);
  assert.equal(isPrototype({ name: "old.HTM", versions: v("application/octet-stream") }), true);
  assert.equal(isPrototype({ name: "landing", versions: v("text/html; charset=utf-8") }), true);
  assert.equal(isPrototype({ name: "landing", versions: v("text/plain; charset=utf-8") }), false);
  assert.equal(isPrototype({ name: "notes.md", versions: v("text/markdown") }), false);
  assert.equal(isPrototype({ name: "empty", versions: [] }), false);
});
