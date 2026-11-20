"use client";

import { useEffect, useSyncExternalStore } from "react";
import { isOnline, subscribeOnline } from "../lib/online";

const subscribe = (cb: () => void) => subscribeOnline(window, cb);
const getSnapshot = () => isOnline(navigator);
const getServerSnapshot = () => true;

/** PwaClient registers the service worker (production only) and shows an offline banner. */
export default function PwaClient() {
  const online = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

  useEffect(() => {
    if (process.env.NODE_ENV === "production" && "serviceWorker" in navigator) {
      navigator.serviceWorker.register("/sw.js").catch(() => {});
    }
  }, []);

  if (online) return null;
  return (
    <div
      role="alert"
      style={{ position: "fixed", inset: "0 0 auto 0", zIndex: 1000, padding: "10px 16px", textAlign: "center", fontWeight: 600, background: "var(--ink)", color: "var(--paper)" }}
    >
      Canopy is offline. Reconnecting when your network is back…
    </div>
  );
}
