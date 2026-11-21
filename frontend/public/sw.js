// Service worker: offline page, web push with action buttons.
const CACHE = "canopy-offline-v2";
const OFFLINE_URL = "/offline.html";
const ICON = "/icons/icon-192.png";
const RESULT_SHOWN_MS = 4000;

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.add(new Request(OFFLINE_URL, { cache: "reload" }))));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  if (event.request.mode !== "navigate") return;
  event.respondWith(fetch(event.request).catch(() => caches.match(OFFLINE_URL)));
});

function readPush(event) {
  if (!event.data) return null;
  try {
    return event.data.json();
  } catch {
    return { title: "Canopy", body: event.data.text() };
  }
}

function openWindows() {
  return self.clients.matchAll({ type: "window", includeUncontrolled: true });
}

self.addEventListener("push", (event) => {
  const msg = readPush(event);
  if (!msg) return;
  event.waitUntil(
    openWindows().then((wins) => {
      // Someone is looking at Canopy: the board already shows it.
      if (wins.some((w) => w.focused && w.visibilityState !== "hidden")) return;
      const max = (self.Notification && self.Notification.maxActions) || 2;
      const actions = (msg.actions || []).slice(0, max).map((a) => ({ action: a.action, title: a.title }));
      return self.registration.showNotification(msg.title || "Canopy", {
        body: msg.body || "",
        tag: msg.tag,
        icon: msg.icon || ICON,
        badge: ICON,
        image: msg.image,
        actions,
        requireInteraction: msg.kind === "questions",
        data: { url: msg.url || "/", token: msg.token || "", signalId: msg.signal_id || "", tag: msg.tag },
      });
    }),
  );
});

async function runAction(data, action) {
  let text = "Couldn’t do that. Open Canopy to answer.";
  try {
    const res = await fetch("/api/v1/notifications/act", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ token: data.token, action }),
    });
    if (res.ok) text = "Done";
    else if (res.status === 409) text = "Already answered";
  } catch {
    // offline: keep the default text
  }
  await self.registration.showNotification("Canopy", {
    body: text,
    tag: data.tag,
    icon: ICON,
    badge: ICON,
    silent: true,
    data: { url: data.url },
  });
  await new Promise((r) => setTimeout(r, RESULT_SHOWN_MS));
  const shown = await self.registration.getNotifications({ tag: data.tag });
  shown.forEach((n) => n.close());
}

async function openTarget(url) {
  const target = new URL(url || "/", self.location.origin).href;
  const wins = await openWindows();
  const win = wins.find((w) => new URL(w.url).origin === self.location.origin);
  if (win) {
    await win.focus();
    win.postMessage({ type: "canopy-open", url: target });
    return;
  }
  await self.clients.openWindow(target);
}

self.addEventListener("notificationclick", (event) => {
  const data = event.notification.data || {};
  event.notification.close();
  if (event.action && event.action !== "open" && data.token) {
    event.waitUntil(runAction(data, event.action));
    return;
  }
  event.waitUntil(openTarget(data.url));
});

self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(resubscribe(event));
});

async function resubscribe(event) {
  const sub = event.newSubscription || (await self.registration.pushManager.subscribe(event.oldSubscription ? event.oldSubscription.options : { userVisibleOnly: true }));
  const csrf = await fetch("/api/v1/auth/csrf", { credentials: "include" }).then((r) => r.json());
  await fetch("/api/v1/push/subscriptions", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf.token },
    body: JSON.stringify({ endpoint: sub.toJSON().endpoint, keys: sub.toJSON().keys }),
  });
}
