/** Minimal slice of the browser's online/offline surface, so it can be faked in tests. */
export interface OnlineSource {
  onLine: boolean;
  addEventListener(type: "online" | "offline", fn: () => void): void;
  removeEventListener(type: "online" | "offline", fn: () => void): void;
}

/** isOnline treats a missing source (server render) as online. */
export function isOnline(nav?: { onLine: boolean }): boolean {
  return nav ? nav.onLine : true;
}

/** subscribeOnline calls fn with the new state on each change; returns an unsubscribe. */
export function subscribeOnline(target: Pick<OnlineSource, "addEventListener" | "removeEventListener">, fn: (online: boolean) => void): () => void {
  const up = () => fn(true);
  const down = () => fn(false);
  target.addEventListener("online", up);
  target.addEventListener("offline", down);
  return () => {
    target.removeEventListener("online", up);
    target.removeEventListener("offline", down);
  };
}
