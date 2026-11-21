import assert from "node:assert/strict";
import { test } from "node:test";
import { centerCam, fitMap, mapBounds, mapToWorld, nodeLook, viewRect } from "./minimap.ts";

const box = { x0: 0, y0: 0, x1: 1000, y1: 500 };

test("the map keeps the tree's proportions and centers it", () => {
  const b = mapBounds(box, 0);
  const f = fitMap(b, 200, 200);
  assert.equal(f.s, 0.2);
  assert.equal(f.ox, 0);
  assert.equal(f.oy, 50);
});

test("clicking a map point gives the tree point under it", () => {
  const f = fitMap(mapBounds(box, 0), 200, 200);
  assert.deepEqual(mapToWorld(f, 100, 100), { x: 500, y: 250 });
  assert.deepEqual(mapToWorld(f, 0, 50), { x: 0, y: 0 });
});

test("centering puts the point in the middle of the view", () => {
  const cam = { ...centerCam(500, 250, 0.5, 800, 600), z: 0.5 };
  const v = viewRect(cam, 800, 600);
  assert.equal(v.x + v.w / 2, 500);
  assert.equal(v.y + v.h / 2, 250);
  assert.equal(v.w, 1600);
});

test("margin gives room around the box", () => {
  const b = mapBounds(box, 0.1);
  assert.equal(b.x0, -100);
  assert.equal(b.y1, 550);
});

test("a saved choice wins, otherwise phones start hidden", async () => {
  const { minimapDefault } = await import("./minimap.ts");
  assert.equal(minimapDefault("1", 390), true);
  assert.equal(minimapDefault("0", 1440), false);
  assert.equal(minimapDefault(null, 390), false);
  assert.equal(minimapDefault(null, 1440), true);
  assert.equal(minimapDefault("junk", 640), true);
});

test("task rects follow status like the board cards", () => {
  assert.equal(nodeLook({ type: "task", taskStatus: "doing" }).fill, "--amber-soft");
  assert.equal(nodeLook({ type: "task", taskStatus: "done" }).fill, "--green-soft");
  assert.equal(nodeLook({ type: "task", taskStatus: "todo" }).fill, "--surface");
  assert.equal(nodeLook({ type: "task", taskStatus: "waiting", blocked: true }).edge, "--red");
  assert.equal(nodeLook({ type: "task", goalColor: "#123456" }).accent, "#123456");
});

test("managers get the team bar, warnings turn it amber", () => {
  assert.deepEqual([nodeLook({ type: "member", mgr: true }).accent, nodeLook({ type: "member", mgr: true }).side], ["--team", "top"]);
  assert.equal(nodeLook({ type: "member", mgr: true, warn: true }).accent, "--amber");
  assert.equal(nodeLook({ type: "member" }).accent, undefined);
});

test("sessions are dark, stickies use their note colour", () => {
  assert.equal(nodeLook({ type: "session", goalColor: "#abc" }).fill, "--term");
  assert.equal(nodeLook({ type: "sticky", stickyColor: "pink" }).fill, "--n-pink");
});
