// Web push for this device: subscribe, unsubscribe, status and a test message.

export type PushApi = <T>(method: "GET" | "POST" | "DELETE", path: string, body?: unknown) => Promise<T>;

/** What push needs from the browser; tests pass a fake. */
export type PushEnv = {
  navigator: {
    userAgent: string;
    maxTouchPoints?: number;
    standalone?: boolean;
    serviceWorker?: { ready: Promise<ServiceWorkerRegistration> };
  };
  hasPushManager: boolean;
  notification?: { permission: NotificationPermission; requestPermission: () => Promise<NotificationPermission> };
  standalone: boolean;
  api: PushApi;
};

export type PushSupport = "ok" | "needs-install" | "unsupported";

export type PushStatus = {
  support: PushSupport;
  permission: NotificationPermission | "unsupported";
  subscribed: boolean;
};

const defaultApi: PushApi = async (method, path, body) => (await import("./api")).api(method, path, body);

export function browserEnv(): PushEnv {
  return {
    navigator: navigator as PushEnv["navigator"],
    hasPushManager: "PushManager" in window,
    notification: typeof Notification === "undefined" ? undefined : Notification,
    standalone: window.matchMedia("(display-mode: standalone)").matches || (navigator as PushEnv["navigator"]).standalone === true,
    api: defaultApi,
  };
}

export function isIOS(nav: Pick<PushEnv["navigator"], "userAgent" | "maxTouchPoints">): boolean {
  return /iPhone|iPad|iPod/.test(nav.userAgent) || (/Macintosh/.test(nav.userAgent) && (nav.maxTouchPoints ?? 0) > 1);
}

/** iPhones and iPads only allow push for apps added to the Home Screen. */
export function pushSupport(env: PushEnv): PushSupport {
  if (isIOS(env.navigator) && !env.standalone) return "needs-install";
  if (!env.navigator.serviceWorker || !env.hasPushManager || !env.notification) return "unsupported";
  return "ok";
}

export const IOS_INSTALL_NOTE = "Add Canopy to your Home Screen to get notifications.";
export const IOS_BUTTONS_NOTE = "Buttons in notifications don’t show on iPhone and iPad. Tap the notification to open Canopy.";

async function currentSubscription(env: PushEnv): Promise<PushSubscription | null> {
  const reg = await env.navigator.serviceWorker!.ready;
  return reg.pushManager.getSubscription();
}

export async function pushStatus(env: PushEnv): Promise<PushStatus> {
  const support = pushSupport(env);
  if (support !== "ok") return { support, permission: env.notification?.permission ?? "unsupported", subscribed: false };
  const sub = await currentSubscription(env);
  return { support, permission: env.notification!.permission, subscribed: sub !== null };
}

/** The hub's key is URL-safe base64; the browser wants bytes. */
export function keyBytes(key: string): Uint8Array<ArrayBuffer> {
  const pad = "=".repeat((4 - (key.length % 4)) % 4);
  const raw = atob((key + pad).replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export class PushError extends Error {}

/** Asks permission, subscribes this browser and tells the hub. */
export async function subscribePush(env: PushEnv): Promise<void> {
  const support = pushSupport(env);
  if (support === "needs-install") throw new PushError(IOS_INSTALL_NOTE);
  if (support === "unsupported") throw new PushError("This browser can’t receive push notifications.");
  const permission = env.notification!.permission === "granted" ? "granted" : await env.notification!.requestPermission();
  if (permission !== "granted") throw new PushError("Notifications are blocked. Allow them in the browser’s site settings.");
  const { key } = await env.api<{ key: string }>("GET", "/push/key");
  const reg = await env.navigator.serviceWorker!.ready;
  const sub =
    (await reg.pushManager.getSubscription()) ??
    (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) }));
  // The hub accepts only these two fields; toJSON also has expirationTime.
  const { endpoint, keys } = sub.toJSON();
  await env.api("POST", "/push/subscriptions", { endpoint, keys });
}

export async function unsubscribePush(env: PushEnv): Promise<void> {
  if (pushSupport(env) !== "ok") return;
  const sub = await currentSubscription(env);
  if (!sub) return;
  await env.api("DELETE", "/push/subscriptions", { endpoint: sub.endpoint });
  await sub.unsubscribe();
}

/** Sends a test notification to this person's devices; resolves with how many took it. */
export async function testPush(env: PushEnv): Promise<number> {
  return (await env.api<{ sent: number }>("POST", "/push/test")).sent;
}

/** True when this device is subscribed, so the in-tab notifications should stay quiet. */
export async function deviceHasPush(env: PushEnv): Promise<boolean> {
  try {
    return (await pushStatus(env)).subscribed;
  } catch {
    return false;
  }
}
