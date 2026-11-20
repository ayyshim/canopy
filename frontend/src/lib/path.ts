"use client";

import { usePathname } from "next/navigation";

/**
 * Reads a dynamic segment from the URL path. The hub serves every /reset/<token>,
 * /w/<id>… from one exported page, so the router's own params would be the placeholder.
 */
export function usePathSegment(prefix: string): string {
  const path = usePathname() ?? "";
  if (!path.startsWith(prefix)) return "";
  return decodeURIComponent(path.slice(prefix.length).split("/")[0] ?? "");
}
