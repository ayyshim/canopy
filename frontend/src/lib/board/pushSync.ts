"use client";

import { useEffect } from "react";
import type { Me } from "@/lib/api";
import { needsMe } from "./derive";
import { useBoard } from "./store";
import { useUI } from "./ui";
import { parseDeepLink, workspaceOf } from "./deeplink";

/** Shows what is waiting for you on the app icon (where the browser supports it). */
export function useAppBadge(me: Me) {
  useEffect(() => {
    const nav = navigator as Navigator & { setAppBadge?: (n?: number) => Promise<void>; clearAppBadge?: () => Promise<void> };
    if (!nav.setAppBadge) return;
    let last = -1;
    const apply = (n: number) => {
      if (n === last) return;
      last = n;
      (n > 0 ? nav.setAppBadge!(n) : nav.clearAppBadge?.())?.catch(() => {});
    };
    const sync = () => {
      const b = useBoard.getState().board;
      if (b) apply(needsMe(b, me.user.id).length);
    };
    sync();
    return useBoard.subscribe(sync);
  }, [me]);
}

/** Closes a phone or desktop notification once its signal is answered, here or elsewhere. */
export function useCloseResolvedNotifications(me: Me) {
  useEffect(() => {
    if (!("serviceWorker" in navigator)) return;
    let waiting = new Set<string>();
    return useBoard.subscribe((state) => {
      const b = state.board;
      if (!b) return;
      const now = new Set(needsMe(b, me.user.id).map((s) => s.id));
      for (const id of waiting) {
        if (!now.has(id)) closeNotifications(id);
      }
      waiting = now;
    });
  }, [me]);
}

function closeNotifications(tag: string) {
  navigator.serviceWorker.getRegistration().then(
    (reg) => reg?.getNotifications({ tag }).then((list) => list.forEach((n) => n.close())),
    () => {},
  );
}

/** Opens and centers what a notification link points at: a goal, a task or a signal's task. */
export function useDeepLink(wsId: string, ready: boolean) {
  useEffect(() => {
    if (!ready) return;
    const apply = (search: string) => {
      const link = parseDeepLink(search);
      const b = useBoard.getState().board;
      const sig = link.signal ? b?.signals.find((s) => s.id === link.signal) : undefined;
      const task = link.task ?? sig?.task_id ?? undefined;
      const goal = link.goal ?? sig?.goal_id ?? undefined;
      const ui = useUI.getState();
      if (task) {
        ui.openSide({ type: "task", id: task });
        ui.centerOn(task);
      } else if (goal) {
        ui.openSide({ type: "goal", id: goal });
        ui.centerOn(goal);
      }
    };
    if (location.search) {
      apply(location.search);
      history.replaceState(null, "", location.pathname);
    }
    const onMessage = (e: MessageEvent) => {
      if (e.data?.type !== "canopy-open" || typeof e.data.url !== "string") return;
      const url = new URL(e.data.url, location.origin);
      if (url.origin !== location.origin) return;
      if (workspaceOf(url.pathname) === wsId) apply(url.search);
      else location.assign(url.href);
    };
    navigator.serviceWorker?.addEventListener("message", onMessage);
    return () => navigator.serviceWorker?.removeEventListener("message", onMessage);
  }, [wsId, ready]);
}
