/** isExternal is true for links that leave the app: a scheme (https:, mailto:, data:) or //host. */
export function isExternal(href: string): boolean {
  return /^([a-z][a-z0-9+.-]*:|\/\/)/i.test(href.trim());
}

/**
 * siblingName turns a path written inside an output into the name of another output.
 * It resolves from the folder of the output being read, so shots/home.png inside docs/report.md
 * means docs/shots/home.png. A leading slash starts from the top. Query and hash are dropped.
 * It returns null for external links, anchors and paths that climb above the top.
 */
export function siblingName(currentName: string, raw: string): string | null {
  const href = raw.trim();
  if (!href || href.startsWith("#") || isExternal(href)) return null;
  let path = href.split(/[?#]/)[0];
  try { path = decodeURIComponent(path); } catch { /* keep the raw path */ }
  const parts = path.startsWith("/") ? [] : currentName.split("/").slice(0, -1);
  for (const seg of path.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      if (!parts.length) return null;
      parts.pop();
    } else parts.push(seg);
  }
  return parts.length ? parts.join("/") : null;
}
