"use client";

import { useSyncExternalStore } from "react";
import { api } from "./api";
import type { components } from "./api-types";
import { SKILL_DESC, SKILL_LABEL } from "./types";

export type SkillCategory = components["schemas"]["SkillCategory"];
export type SkillSub = SkillCategory["subs"][number];
export type BaseRole = SkillSub["base"];

let catalog: SkillCategory[] = [];
let index = new Map<string, SkillSub & { category: string }>();
let started = false;
const listeners = new Set<() => void>();

function set(c: SkillCategory[]) {
  catalog = c;
  index = new Map(c.flatMap((cat) => cat.subs.map((s) => [s.key, { ...s, category: cat.label }] as const)));
  listeners.forEach((l) => l());
}

function load() {
  if (started) return;
  started = true;
  api<{ catalog?: SkillCategory[] }>("GET", "/clis").then((r) => set(r.catalog ?? [])).catch(() => { started = false; });
}

/** Returns the catalog of "good at" categories and loads it once. */
export function useCatalog(): SkillCategory[] {
  load();
  return useSyncExternalStore((l) => { listeners.add(l); return () => { listeners.delete(l); }; }, () => catalog, () => catalog);
}

const pretty = (k: string) => { const s = k.split(".").pop() ?? k; return s ? s[0].toUpperCase() + s.slice(1).replace(/[-_]/g, " ") : "Staff"; };

/** The label for a skill key; old plain keys and keys not in the catalog still get a name. */
export function skillLabel(key?: string | null): string {
  if (!key) return "Staff";
  return index.get(key)?.label ?? SKILL_LABEL[key] ?? pretty(key);
}

export function skillDesc(key?: string | null): string {
  if (!key) return "";
  return index.get(key)?.desc ?? SKILL_DESC[key] ?? "";
}

/** The plain role a skill key builds on, for folder defaults and starter instructions. */
export function baseRole(key?: string | null): string {
  if (!key) return "";
  return index.get(key)?.base ?? (key.includes(".") ? "" : key);
}
