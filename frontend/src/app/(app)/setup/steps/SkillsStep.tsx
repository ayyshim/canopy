"use client";

import { SkillAdd, SkillList, SkillsIntro } from "@/components/skills/Skills";
import { newKey } from "@/lib/setup/state";
import type { SkillView } from "@/lib/skills";
import type { StepProps } from "../Setup";

/** SkillsStep builds the workspace skill library. Optional: people can add skills later in settings. */
export function SkillsStep({ p, set, orgId }: StepProps) {
  // The shared list shows the same shape the hub returns; setup keeps the key as the id.
  const view: SkillView[] = p.skills.map((s) => ({ id: s.key, name: s.name, source_kind: s.source_kind, source_url: s.source_url, content: s.content, updated_at: "" }));
  const used = (s: SkillView) => p.members.filter((m) => m.skill_keys.includes(s.id)).length;
  return (
    <>
      <h1>Skills <span className="hint" style={{ fontWeight: 500, fontSize: 15 }}>(optional)</span></h1>
      <SkillsIntro />
      <p className="hint" style={{ margin: 0 }}>You’ll choose who gets which skill on the next step. Not sure yet? Skip this and add skills later in workspace settings.</p>
      <SkillList skills={view} usage={used}
        onRename={async (s, name) => {
          if (p.skills.some((x) => x.key !== s.id && x.name.toLowerCase() === name.toLowerCase())) throw new Error("You already have a skill with that name.");
          set((x) => ({ ...x, skills: x.skills.map((k) => (k.key === s.id ? { ...k, name } : k)) }));
        }}
        onDelete={async (s) => set((x) => ({ ...x, skills: x.skills.filter((k) => k.key !== s.id), members: x.members.map((m) => ({ ...m, skill_keys: m.skill_keys.filter((k) => k !== s.id) })) }))} />
      <SkillAdd orgId={orgId} taken={p.skills.map((s) => s.name)}
        onAdd={async (d) => set((x) => ({ ...x, skills: [...x.skills, { key: newKey("k"), ...d }] }))} />
    </>
  );
}
