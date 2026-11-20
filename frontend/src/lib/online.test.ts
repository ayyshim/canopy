import assert from "node:assert/strict";
import { test } from "node:test";
import { isOnline, subscribeOnline } from "./online.ts";

test("missing navigator counts as online", () => {
  assert.equal(isOnline(undefined), true);
});

test("reads navigator.onLine", () => {
  assert.equal(isOnline({ onLine: false }), false);
  assert.equal(isOnline({ onLine: true }), true);
});

test("subscribe reports changes and unsubscribes", () => {
  const target = new EventTarget();
  const seen: boolean[] = [];
  const off = subscribeOnline(target, (v) => seen.push(v));
  target.dispatchEvent(new Event("offline"));
  target.dispatchEvent(new Event("online"));
  off();
  target.dispatchEvent(new Event("offline"));
  assert.deepEqual(seen, [false, true]);
});
