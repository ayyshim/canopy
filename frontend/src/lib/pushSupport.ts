/** What this browser can do about push, shown as the status line of “This device”. */
export type PushAvailability = "unsupported" | "install" | "blocked" | "ready";

export type PushEnv = {
  hasServiceWorker: boolean;
  hasPushManager: boolean;
  hasNotification: boolean;
  permission: NotificationPermission | "none";
  ios: boolean;
  standalone: boolean;
};

export function pushAvailability(e: PushEnv): PushAvailability {
  // iPhone and iPad only allow push from an app added to the Home Screen.
  if (e.ios && !e.standalone) return "install";
  if (!e.hasServiceWorker || !e.hasPushManager || !e.hasNotification) return "unsupported";
  if (e.permission === "denied") return "blocked";
  return "ready";
}

export function currentEnv(): PushEnv {
  const ua = navigator.userAgent;
  const ios = /iPad|iPhone|iPod/.test(ua) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  return {
    hasServiceWorker: "serviceWorker" in navigator,
    hasPushManager: "PushManager" in window,
    hasNotification: "Notification" in window,
    permission: "Notification" in window ? Notification.permission : "none",
    ios,
    standalone: window.matchMedia("(display-mode: standalone)").matches || (navigator as Navigator & { standalone?: boolean }).standalone === true,
  };
}

export function keyToBytes(key: string): Uint8Array<ArrayBuffer> {
  const b64 = (key + "=".repeat((4 - (key.length % 4)) % 4)).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(b64);
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}
