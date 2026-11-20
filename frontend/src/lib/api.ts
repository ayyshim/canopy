import type { components } from "./api-types";

export type Me = components["schemas"]["Me"];
export type User = components["schemas"]["User"];
export type OrgSummary = components["schemas"]["OrgSummary"];
export type Organization = components["schemas"]["Organization"];
export type OrgResponse = components["schemas"]["OrgResponse"];
export type OrgSize = components["schemas"]["OrgSize"];
export type Providers = components["schemas"]["Providers"];
export type Hub = components["schemas"]["Hub"];
export type HubSettings = components["schemas"]["HubSettings"];
export type ProviderInput = components["schemas"]["ProviderInput"];
export type AdminOverview = components["schemas"]["AdminOverview"];
export type AdminOrg = components["schemas"]["AdminOrg"];
export type AdminOrgDetail = components["schemas"]["AdminOrgDetail"];
export type AdminUser = components["schemas"]["AdminUser"];
export type AdminUserDetail = components["schemas"]["AdminUserDetail"];
export type Page<T> = { items: T[]; total: number };

/** An error response from the hub: { error: { code, message, fields } }. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields: Record<string, string> = {},
  ) {
    super(message);
  }
}

const CSRF_COOKIE = "canopy_csrf";

function readCookie(name: string): string | undefined {
  return document.cookie
    .split("; ")
    .find((c) => c.startsWith(name + "="))
    ?.slice(name.length + 1);
}

async function csrfToken(refresh = false): Promise<string> {
  const existing = refresh ? undefined : readCookie(CSRF_COOKIE);
  if (existing) return existing;
  const res = await fetch("/api/v1/auth/csrf", { credentials: "same-origin" });
  const data = (await res.json()) as { token: string };
  return data.token;
}

type Method = "GET" | "POST" | "PATCH" | "PUT" | "DELETE";

/** Calls the hub REST API. Unsafe methods carry the CSRF header; errors throw ApiError. */
export async function api<T>(method: Method, path: string, body?: unknown, retried = false): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET") headers["X-CSRF-Token"] = await csrfToken(retried);

  const res = await fetch("/api/v1" + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = data?.error ?? {};
    if (err.code === "csrf" && !retried) return api<T>(method, path, body, true);
    throw new ApiError(res.status, err.code ?? "internal", err.message ?? "Something went wrong. Try again.", err.fields ?? {});
  }
  return data as T;
}

/** Sends a file as the raw request body (logos, attachments). */
export async function uploadFile<T>(path: string, file: File, method: "PUT" | "POST" = "PUT", retried = false): Promise<T> {
  const res = await fetch("/api/v1" + path, {
    method,
    headers: { Accept: "application/json", "Content-Type": file.type || "application/octet-stream", "X-CSRF-Token": await csrfToken(retried) },
    body: file,
    credentials: "same-origin",
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = data?.error ?? {};
    if (err.code === "csrf" && !retried) return uploadFile<T>(path, file, method, true);
    throw new ApiError(res.status, err.code ?? "internal", err.message ?? "Something went wrong. Try again.", err.fields ?? {});
  }
  return data as T;
}

export const ORG_SIZES: { value: OrgSize; label: string }[] = [
  { value: "just_me", label: "Just me" },
  { value: "2_10", label: "2 to 10" },
  { value: "11_50", label: "11 to 50" },
  { value: "51_plus", label: "51 or more" },
];

export const ROLE_LABEL: Record<string, string> = { owner: "Owner", admin: "Admin", member: "Member" };

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "") + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase() || "?";
}

export function currentOrg(me: Me): OrgSummary | undefined {
  return me.organizations.find((o) => o.id === me.current_org_id);
}

export function isOrgAdmin(role: string | undefined): boolean {
  return role === "owner" || role === "admin";
}

/**
 * Full page load. Used after sign-in, sign-out and other identity changes: Next.js keeps visited
 * routes mounted in the background, so a client-side push could show the previous user's state.
 */
export function hardNavigate(path: string) {
  window.location.assign(path);
}

/** Where a signed-in person lands: their organization, the hub dashboard, or creating one. */
export function landingPath(me: Me): string {
  if (me.current_org_id) return "/board";
  return me.user.is_hub_admin ? "/admin" : "/new-org";
}

const dateFmt = new Intl.DateTimeFormat(undefined, { year: "numeric", month: "short", day: "numeric" });

export function formatDate(iso: string): string {
  return dateFmt.format(new Date(iso));
}

export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "Never";
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours} h ago`;
  const days = Math.round(hours / 24);
  return days < 30 ? `${days} d ago` : formatDate(iso);
}
