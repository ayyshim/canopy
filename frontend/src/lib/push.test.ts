import assert from "node:assert/strict";
import { test } from "node:test";
import {
  deviceHasPush, isIOS, keyBytes, pushStatus, pushSupport, subscribePush, testPush, unsubscribePush,
  type PushEnv,
} from "./push.ts";

const CHROME = "Mozilla/5.0 (X11; Linux x86_64) Chrome/130";
const IPHONE = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Safari/605";

type Call = { method: string; path: string; body?: unknown };

function fake(opts: { ua?: string; standalone?: boolean; permission?: NotificationPermission; subscribed?: boolean; noPush?: boolean } = {}) {
  const calls: Call[] = [];
  const log: string[] = [];
  let sub: unknown = opts.subscribed ? makeSub() : null;
  function makeSub() {
    return {
      endpoint: "https://push.example/abc",
      toJSON: () => ({ endpoint: "https://push.example/abc", expirationTime: null, keys: { p256dh: "p", auth: "a" } }),
      unsubscribe: async () => { log.push("unsubscribed"); sub = null; return true; },
    };
  }
  const reg = {
    pushManager: {
      getSubscription: async () => sub,
      subscribe: async (o: { userVisibleOnly: boolean; applicationServerKey: Uint8Array }) => {
        log.push(`subscribe:${o.userVisibleOnly}:${o.applicationServerKey.length}`);
        sub = makeSub();
        return sub;
      },
    },
  };
  const env: PushEnv = {
    navigator: { userAgent: opts.ua ?? CHROME, serviceWorker: { ready: Promise.resolve(reg as unknown as ServiceWorkerRegistration) } },
    hasPushManager: !opts.noPush,
    notification: { permission: opts.permission ?? "default", requestPermission: async () => { log.push("asked"); return opts.permission === "denied" ? "denied" : "granted"; } },
    standalone: opts.standalone ?? false,
    api: (async (method: string, path: string, body?: unknown) => {
      calls.push({ method, path, body });
      if (path === "/push/key") return { key: "AQID" };
      if (path === "/push/test") return { sent: 2 };
      return {};
    }) as PushEnv["api"],
  };
  return { env, calls, log };
}

test("keyBytes decodes URL-safe base64", () => {
  assert.deepEqual([...keyBytes("AQID")], [1, 2, 3]);
  assert.deepEqual([...keyBytes("-_8")], [251, 255]);
});

test("iOS detection and support", () => {
  assert.equal(isIOS({ userAgent: IPHONE }), true);
  assert.equal(isIOS({ userAgent: CHROME }), false);
  assert.equal(pushSupport(fake({ ua: IPHONE }).env), "needs-install");
  assert.equal(pushSupport(fake({ ua: IPHONE, standalone: true }).env), "ok");
  assert.equal(pushSupport(fake({ noPush: true }).env), "unsupported");
  assert.equal(pushSupport(fake().env), "ok");
});

test("status reflects permission and subscription", async () => {
  assert.deepEqual(await pushStatus(fake({ permission: "granted", subscribed: true }).env), { support: "ok", permission: "granted", subscribed: true });
  assert.equal((await pushStatus(fake().env)).subscribed, false);
  assert.equal((await pushStatus(fake({ ua: IPHONE }).env)).support, "needs-install");
  assert.equal(await deviceHasPush(fake({ subscribed: true }).env), true);
});

test("subscribe asks permission, uses the hub key and saves the subscription", async () => {
  const f = fake();
  await subscribePush(f.env);
  assert.deepEqual(f.log, ["asked", "subscribe:true:3"]);
  assert.deepEqual(f.calls.map((c) => `${c.method} ${c.path}`), ["GET /push/key", "POST /push/subscriptions"]);
  assert.deepEqual(f.calls[1].body, { endpoint: "https://push.example/abc", keys: { p256dh: "p", auth: "a" } });
});

test("subscribe reuses an existing subscription", async () => {
  const f = fake({ permission: "granted", subscribed: true });
  await subscribePush(f.env);
  assert.deepEqual(f.log, []);
  assert.equal(f.calls.at(-1)?.path, "/push/subscriptions");
});

test("subscribe refuses when blocked or on iOS without install", async () => {
  await assert.rejects(subscribePush(fake({ permission: "denied" }).env), /blocked/);
  await assert.rejects(subscribePush(fake({ ua: IPHONE }).env), /Home Screen/);
  await assert.rejects(subscribePush(fake({ noPush: true }).env), /can’t receive/);
});

test("unsubscribe tells the hub, then the browser", async () => {
  const f = fake({ permission: "granted", subscribed: true });
  await unsubscribePush(f.env);
  assert.deepEqual(f.calls, [{ method: "DELETE", path: "/push/subscriptions", body: { endpoint: "https://push.example/abc" } }]);
  assert.deepEqual(f.log, ["unsubscribed"]);
  const none = fake();
  await unsubscribePush(none.env);
  assert.equal(none.calls.length, 0);
});

test("test push returns how many devices took it", async () => {
  assert.equal(await testPush(fake().env), 2);
});
