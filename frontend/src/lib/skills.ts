import { api, ApiError } from "./api";
import type { SkillView } from "./core-types";

export type { SkillView };

/** What someone is adding to the library. GitHub skills carry the markdown that was fetched. */
export type SkillDraft = { name: string; source_kind: "md" | "github"; source_url: string; content: string };

export const MAX_SKILL_BYTES = 100 * 1024;

/** The first message the hub gave for a failed call, in words a person can act on. */
export function friendly(e: unknown, fallback = "Something went wrong. Try again."): string {
  if (e instanceof ApiError) return Object.values(e.fields)[0] ?? e.message;
  if (e instanceof Error && e.message) return e.message;
  return fallback;
}

/** Reads a markdown file someone picked, refusing anything that isn't text or is too big. */
export async function readMarkdownFile(f: File): Promise<{ name: string; content: string }> {
  if (f.size > MAX_SKILL_BYTES) throw new Error(`“${f.name}” is too big. Skills must be under ${MAX_SKILL_BYTES / 1024} KB.`);
  if (!/\.(md|markdown|txt)$/i.test(f.name)) throw new Error("Choose a markdown file (it ends in .md).");
  const content = await f.text();
  if (!content.trim()) throw new Error(`“${f.name}” is empty.`);
  return { name: nameFromFile(f.name), content };
}

export function nameFromFile(file: string): string {
  const base = file.replace(/\.(md|markdown|txt)$/i, "").replace(/[-_]+/g, " ").trim();
  return base ? base[0].toUpperCase() + base.slice(1) : "";
}

/** previewGithub fetches a GitHub link through the hub, before anything is saved. */
export function previewGithub(orgId: string, url: string): Promise<{ name: string; content: string }> {
  return api("POST", `/orgs/${orgId}/skills/preview`, { source_url: url });
}

export const skillNames = (ids: string[] | undefined, library: SkillView[]) =>
  (ids ?? []).map((id) => library.find((s) => s.id === id)?.name).filter((n): n is string => !!n);
