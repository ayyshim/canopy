import assert from "node:assert/strict";
import { test } from "node:test";
import { keyToBytes, pushAvailability, type PushEnv } from "./pushSupport.ts";

const ok: PushEnv = { hasServiceWorker: true, hasPushManager: true, hasNotification: true, permission: "default", ios: false, standalone: false };

test("push is ready in a normal browser", () => {
  assert.equal(pushAvailability(ok), "ready");
  assert.equal(pushAvailability({ ...ok, permission: "granted" }), "ready");
});

test("blocked when the person refused permission", () => {
  assert.equal(pushAvailability({ ...ok, permission: "denied" }), "blocked");
});

test("unsupported without a service worker, push or notifications", () => {
  for (const k of ["hasServiceWorker", "hasPushManager", "hasNotification"] as const) {
    assert.equal(pushAvailability({ ...ok, [k]: false }), "unsupported");
  }
});

test("iPhone asks to install first, and works once installed", () => {
  assert.equal(pushAvailability({ ...ok, ios: true, hasPushManager: false }), "install");
  assert.equal(pushAvailability({ ...ok, ios: true, standalone: true }), "ready");
});

test("the server key becomes bytes", () => {
  assert.deepEqual([...keyToBytes("AQID")], [1, 2, 3]);
  assert.deepEqual([...keyToBytes("-_8")], [251, 255]);
});
