import { api } from "./api";
import { keyToBytes } from "./pushSupport";

export { currentEnv, pushAvailability, type PushAvailability } from "./pushSupport";

async function registration(): Promise<ServiceWorkerRegistration> {
  return (await navigator.serviceWorker.getRegistration()) ?? (await navigator.serviceWorker.register("/sw.js"));
}

/** The push subscription of this browser, if it has one. */
export async function currentSubscription(): Promise<PushSubscription | null> {
  if (!("serviceWorker" in navigator) || !("PushManager" in window)) return null;
  const reg = await navigator.serviceWorker.getRegistration();
  return reg ? reg.pushManager.getSubscription() : null;
}

/** Asks permission, subscribes this browser and tells the hub. Returns false if permission was refused. */
export async function turnOnPush(): Promise<boolean> {
  if ((await Notification.requestPermission()) !== "granted") return false;
  const { key } = await api<{ key: string }>("GET", "/push/key");
  const reg = await registration();
  await navigator.serviceWorker.ready;
  const sub = (await reg.pushManager.getSubscription()) ?? (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyToBytes(key) }));
  await api("POST", "/push/subscriptions", sub.toJSON());
  return true;
}

export async function turnOffPush(): Promise<void> {
  const sub = await currentSubscription();
  if (!sub) return;
  await api("DELETE", "/push/subscriptions", { endpoint: sub.endpoint }).catch(() => {});
  await sub.unsubscribe();
}

export async function sendTestPush(): Promise<void> {
  await api("POST", "/push/test");
}
