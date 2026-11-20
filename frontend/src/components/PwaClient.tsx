"use client";

import { useEffect, useState } from "react";
import { isOnline, subscribeOnline } from "../lib/online";

/** PwaClient registers the service worker (production only) and shows an offline banner. */
export default function PwaClient() {
  const [online, setOnline] = useState(true);

  useEffect(() => {
    if (process.env.NODE_ENV === "production" && "serviceWorker" in navigator) {
      navigator.serviceWorker.register("/sw.js").catch(() => {});
    }
    setOnline(isOnline(navigator));
    return subscribeOnline(window, setOnline);
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
