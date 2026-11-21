"use client";

import { useEffect } from "react";
import type { Me } from "@/lib/api";
import type { SignalView } from "@/lib/types";
import { needsMe, payload, visibleMember } from "./derive";
import { useBoard } from "./store";
import { useUI } from "./ui";

/** The notification row a signal belongs to (same as the hub's emails). */
export function notificationKind(type: string): string {
  if (type === "error" || type === "git") return "errors";
  if (type === "goal_ready") return "goal_ready";
  return "questions";
}

/** useDesktopNotifications shows a desktop notification for each new thing that needs you
 * while this tab is in the background, if you turned them on. With push on for this device
 * the push notification does that job, so nothing shows twice. */
export function useDesktopNotifications(me: Me, pushOn = false) {
  useEffect(() => {
    let seen: Set<string> | null = null;
    return useBoard.subscribe((state) => {
      const b = state.board;
      if (!b) return;
      if (!seen) {
        seen = new Set(b.signals.map((s) => s.id)); // what was there on load isn't new
        return;
      }
      for (const s of needsMe(b, me.user.id)) {
        if (seen.has(s.id)) continue;
        seen.add(s.id);
        const want = me.user.notifications?.[notificationKind(s.type)]?.desktop ?? true;
        if (pushOn || !want || !document.hidden || typeof Notification === "undefined" || Notification.permission !== "granted") continue;
        const name = visibleMember(b, s.member_id)?.name ?? "An agent";
        const n = new Notification(title(s, name), { body: body(s), tag: s.id });
        n.onclick = () => {
          window.focus();
          const at = s.task_id ?? s.goal_id;
          if (at) useUI.getState().centerOn(at);
          n.close();
        };
      }
    });
  }, [me, pushOn]);
}

function title(s: SignalView, name: string): string {
  const p = payload(s);
  switch (s.type) {
    case "question": return `${name} has a question`;
    case "gate": return `${name} wants to run a command`;
    case "dir_access": return `${name} asks for access`;
    case "review": return `Review “${p.title}”`;
    case "owner_change": return "Your approval for a change";
    case "sync_conflict": return "Sync conflict";
    case "error": return `${name} stopped on an error`;
    case "goal_ready": return `“${p.title}” is ready`;
    case "usage_limit": return "A usage limit was reached";
    case "cap": return "Daily token cap";
  }
  return "Something needs you";
}

function body(s: SignalView): string {
  const p = payload(s);
  return String(p.question ?? p.summary ?? p.path ?? p.message ?? p.description ?? "");
}
