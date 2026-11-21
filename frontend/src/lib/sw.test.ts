/* eslint-disable @typescript-eslint/no-explicit-any -- loose mocks of the worker scope */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";

type Handler = (e: any) => void;
type Shown = { title: string; opts: any };

const source = readFileSync(new URL("../../public/sw.js", import.meta.url), "utf8");

function load(opts: { windows?: any[]; fetchStatus?: number; maxActions?: number } = {}) {
  const handlers: Record<string, Handler> = {};
  const shown: Shown[] = [];
  const closed: string[] = [];
  const opened: string[] = [];
  const fetched: { url: string; init: any }[] = [];
  const windows = opts.windows ?? [];
  const self: any = {
    location: { origin: "https://canopy.test" },
    Notification: { maxActions: opts.maxActions ?? 2 },
    addEventListener: (n: string, h: Handler) => (handlers[n] = h),
    skipWaiting: () => {},
    clients: { matchAll: async () => windows, openWindow: async (u: string) => { opened.push(u); }, claim: async () => {} },
    registration: {
      showNotification: async (title: string, o: any) => { shown.push({ title, opts: o }); },
      getNotifications: async () => [{ close: () => closed.push("closed") }],
      pushManager: { subscribe: async () => ({ toJSON: () => ({ endpoint: "e", expirationTime: null, keys: { auth: "a", p256dh: "p" } }) }) },
    },
  };
  const ctx = vm.createContext({
    self, URL, Request, JSON, Promise, Error,
    caches: { open: async () => ({ add: async () => {} }), keys: async () => [], match: async () => null },
    setTimeout: (f: () => void) => { f(); return 0; },
    fetch: async (url: string, init: any) => {
      fetched.push({ url, init });
      if (url.endsWith("/auth/csrf")) return { ok: true, status: 200, json: async () => ({ token: "csrf1" }) };
      return { ok: (opts.fetchStatus ?? 200) < 300, status: opts.fetchStatus ?? 200, json: async () => ({}) };
    },
  });
  vm.runInContext(source, ctx);
  const fire = async (name: string, event: any) => {
    let p: Promise<unknown> = Promise.resolve();
    handlers[name]({ ...event, waitUntil: (x: Promise<unknown>) => { p = x; } });
    await p;
  };
  return { fire, shown, closed, opened, fetched };
}

const push = (msg: object) => ({ data: { json: () => msg, text: () => "plain" } });
const gate = {
  title: "Payments - Dev", body: "rm -rf build", tag: "sig1", url: "/w/1?signal=sig1", icon: "/i.png", kind: "questions",
  token: "tok", signal_id: "sig1", actions: [{ action: "allow", title: "Allow" }, { action: "deny", title: "Deny" }],
};

test("push shows a notification with actions, tag and data", async () => {
  const t = load();
  await t.fire("push", push(gate));
  assert.equal(t.shown.length, 1);
  const { title, opts } = t.shown[0];
  assert.equal(title, "Payments - Dev");
  assert.equal(opts.tag, "sig1");
  assert.equal(opts.body, "rm -rf build");
  assert.equal(opts.requireInteraction, true);
  assert.deepEqual(opts.actions.map((a: any) => a.action), ["allow", "deny"]);
  assert.equal(opts.data.token, "tok");
  assert.equal(opts.badge, "/icons/icon-192.png");
});

test("only questions need interaction, and actions are limited to what the browser shows", async () => {
  const t = load({ maxActions: 1 });
  await t.fire("push", push({ ...gate, kind: "progress" }));
  assert.equal(t.shown[0].opts.requireInteraction, false);
  assert.equal(t.shown[0].opts.actions.length, 1);
});

test("push is skipped when a Canopy window is focused", async () => {
  const t = load({ windows: [{ focused: true, visibilityState: "visible" }] });
  await t.fire("push", push(gate));
  assert.equal(t.shown.length, 0);
  const t2 = load({ windows: [{ focused: false, visibilityState: "visible" }] });
  await t2.fire("push", push(gate));
  assert.equal(t2.shown.length, 1);
});

test("plain text push still shows", async () => {
  const t = load();
  await t.fire("push", { data: { json: () => { throw new Error("no"); }, text: () => "hello" } });
  assert.equal(t.shown[0].opts.body, "hello");
});

function click(action: string, data = { url: "/w/1?signal=sig1", token: "tok", tag: "sig1" }) {
  return { action, notification: { data, close: () => {} } };
}

test("an action button posts the token and shows Done", async () => {
  const t = load();
  await t.fire("notificationclick", click("allow"));
  assert.equal(t.fetched[0].url, "/api/v1/notifications/act");
  assert.equal(t.fetched[0].init.credentials, "include");
  assert.deepEqual(JSON.parse(t.fetched[0].init.body), { token: "tok", action: "allow" });
  assert.equal(t.shown.at(-1)?.opts.body, "Done");
  assert.equal(t.shown.at(-1)?.opts.tag, "sig1");
  assert.equal(t.closed.length, 1);
  assert.equal(t.opened.length, 0);
});

test("a closed signal says Already answered, other failures say to open Canopy", async () => {
  const a = load({ fetchStatus: 409 });
  await a.fire("notificationclick", click("deny"));
  assert.equal(a.shown.at(-1)?.opts.body, "Already answered");
  const b = load({ fetchStatus: 403 });
  await b.fire("notificationclick", click("deny"));
  assert.match(b.shown.at(-1)?.opts.body, /Open Canopy/);
  assert.equal(b.shown.at(-1)?.opts.requireInteraction, true);
  assert.equal(b.closed.length, 0);
});

test("a plain click opens the deep link, or focuses an open tab", async () => {
  const t = load();
  await t.fire("notificationclick", click(""));
  assert.deepEqual(t.opened, ["https://canopy.test/w/1?signal=sig1"]);
  assert.equal(t.fetched.length, 0);

  const sent: any[] = [];
  let focused = false;
  const win = { url: "https://canopy.test/w/1", focus: async () => { focused = true; }, postMessage: (m: any) => sent.push(m) };
  const t2 = load({ windows: [win] });
  await t2.fire("notificationclick", click("open"));
  assert.equal(focused, true);
  assert.deepEqual(JSON.parse(JSON.stringify(sent)), [{ type: "canopy-open", url: "https://canopy.test/w/1?signal=sig1" }]);
  assert.equal(t2.opened.length, 0);
});

test("pushsubscriptionchange saves the new subscription with a CSRF token", async () => {
  const t = load();
  await t.fire("pushsubscriptionchange", { oldSubscription: { options: { userVisibleOnly: true } } });
  const post = t.fetched.at(-1)!;
  assert.equal(post.url, "/api/v1/push/subscriptions");
  assert.equal(post.init.headers["X-CSRF-Token"], "csrf1");
  assert.deepEqual(JSON.parse(post.init.body), { endpoint: "e", keys: { auth: "a", p256dh: "p" } });
});
