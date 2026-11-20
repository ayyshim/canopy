// Fallback directory suggestions (SPEC "Suggestion rules"), the same rules the hub uses
// (hub/internal/domain). In setup no agent is running yet, so these are what the manager
// "suggests".

export type Kind = "code" | "docs" | "design" | "other";
export type Mode = "read" | "read_write";
export type SetupDir = { key: string; path: string; name: string; kind: Kind };
export type Suggestion = { directory_key: string; mode: Mode; reason: string };

const has = (s: string, ...subs: string[]) => subs.some((x) => s.includes(x));

export function dirName(p: string): string {
  const parts = p.replace(/\\/g, "/").replace(/\/+$/, "").split("/");
  return parts[parts.length - 1] || p;
}

export function classify(p: string): Kind {
  const base = dirName(p).toLowerCase();
  if (has(base, "doc", "wiki", "handbook", "notes", "guide", "spec", "content", "blog")) return "docs";
  if (has(base, "design", "figma", "sketch", "mock", "brand", "asset")) return "design";
  return "code";
}

const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, "");

export function mainCodebase(dirs: SetupDir[], workspaceName: string): SetupDir | undefined {
  const want = norm(workspaceName);
  const code = dirs.filter((d) => d.kind === "code");
  return code.find((d) => want && (norm(d.name) === want || norm(d.name).includes(want) || want.includes(norm(d.name)))) ?? code[0];
}

const rank = (m: string) => (m === "read_write" ? 2 : m === "read" ? 1 : 0);

export function clamp(gs: Suggestion[], limit: { directory_key: string; mode: Mode }[] | null): Suggestion[] {
  if (!limit) return gs;
  const max = new Map(limit.map((g) => [g.directory_key, g.mode]));
  return gs.flatMap((g) => {
    const m = max.get(g.directory_key);
    if (!m) return [];
    return [{ ...g, mode: rank(g.mode) > rank(m) ? m : g.mode }];
  });
}

export function suggest(skill: string, dirs: SetupDir[], workspaceName: string, limit: { directory_key: string; mode: Mode }[] | null): Suggestion[] {
  const out: Suggestion[] = [];
  const add = (d: SetupDir, mode: Mode, reason: string) => out.push({ directory_key: d.key, mode, reason });
  switch (skill) {
    case "developer":
    case "reviewer": {
      const d = mainCodebase(dirs, workspaceName);
      if (d) add(d, skill === "developer" ? "read_write" : "read", skill === "developer" ? "Writes code in the main codebase" : "Reviews changes in the main codebase");
      break;
    }
    case "designer":
      dirs.forEach((d) => {
        if (d.kind === "design") add(d, "read_write", "Design files");
        else if (d.kind === "docs") add(d, "read_write", "Documents design decisions");
      });
      break;
    case "writer":
      dirs.filter((d) => d.kind === "docs").forEach((d) => add(d, "read_write", "Writes the docs"));
      break;
    case "researcher":
      dirs.filter((d) => d.kind === "docs" || d.kind === "code").forEach((d) => add(d, "read", "Reads docs and code for research"));
      break;
  }
  return clamp(out, limit);
}
