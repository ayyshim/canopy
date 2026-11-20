"use client";

import { useEffect } from "react";
import { liveIconHref, tabTitle } from "./liveStatus";

// Sets the tab title and shows the LIVE favicon. Restores both on unmount.
export function useLiveTab(org: string | undefined, workspace: string, live: boolean) {
  useEffect(() => {
    const prevTitle = document.title;
    return () => { document.title = prevTitle; };
  }, []);

  useEffect(() => {
    if (workspace) document.title = tabTitle(org, workspace, live);
  }, [org, workspace, live]);

  useEffect(() => {
    if (!live) return;
    const icons = Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]'));
    const link = document.createElement("link");
    link.rel = "icon";
    link.type = "image/svg+xml";
    link.href = liveIconHref(true);
    link.dataset.canopyLive = "1";
    const saved = icons.map((l) => [l, l.disabled] as const);
    icons.forEach((l) => { l.disabled = true; });
    document.head.appendChild(link);
    return () => {
      link.remove();
      saved.forEach(([l, d]) => { l.disabled = d; });
    };
  }, [live]);
}
