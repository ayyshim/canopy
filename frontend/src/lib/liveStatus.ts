// Helpers for the tab title and the LIVE favicon.

export type ActivityInput = {
  tasks: ReadonlyArray<{ status: string }>;
  goals: ReadonlyArray<{ status: string }>;
};

// Active means a task or goal is in progress.
export function isWorkspaceActive(b: ActivityInput): boolean {
  return b.tasks.some((t) => t.status === "doing") || b.goals.some((g) => g.status === "doing" || g.status === "active");
}

// "{ORG} > {WORKSPACE}", plus "● LIVE" when active.
export function tabTitle(org: string | undefined, workspace: string, live: boolean): string {
  const base = org ? `${org} > ${workspace}` : workspace;
  return live ? `${base} · ● LIVE` : base;
}

// The logo, with a green dot when live.
export function liveIconSvg(live: boolean): string {
  const dot = live ? `<circle cx="25" cy="7" r="6.5" fill="#22C55E" stroke="#fff" stroke-width="1.8"/>` : "";
  return `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="#16794F"/><path d="M16 11v5M7 16h18M7 16v4M16 16v4M25 16v4" stroke="#fff" stroke-width="2.2" fill="none" stroke-linecap="round"/><rect x="11" y="4.5" width="10" height="7" rx="2" fill="#fff"/><rect x="3.5" y="19" width="7.5" height="7.5" rx="2" fill="#FFE58A"/><rect x="12.25" y="19" width="7.5" height="7.5" rx="2" fill="#BFE6D1"/><rect x="21" y="19" width="7.5" height="7.5" rx="2" fill="#D9D0FA"/>${dot}</svg>`;
}

export const liveIconHref = (live: boolean) => `data:image/svg+xml,${encodeURIComponent(liveIconSvg(live))}`;
