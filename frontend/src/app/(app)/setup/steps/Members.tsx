"use client";

import { useState } from "react";
import { Avatar } from "@/components/board/icons";
import { Markdown } from "@/components/board/panels/bits";
import { SkillPicker } from "@/components/skills/Skills";
import { defaultInstructions } from "@/components/board/chat/ChatSettings";
import { newKey, type PickedDir, type SetupMember, type SetupPayload } from "@/lib/setup/state";
import { clamp, suggest } from "@/lib/setup/suggest";
import { SKILL_LABEL } from "@/lib/types";
import type { StepProps } from "../Setup";

const WHY: Record<string, string> = {
  developer: "builds in the main codebase, so only that is shared for now", reviewer: "reviews changes in the main codebase",
  designer: "works on design files and docs", writer: "writes in the docs", researcher: "reads docs and code to research",
};

function member(kind: "manager" | "staff", name: string, skill = "", reports = ""): SetupMember {
  return { key: newKey("m"), name, kind, skill, reports_to_key: reports, cli: "claude", model: kind === "manager" ? "opus" : "sonnet",
    all_directories: kind === "manager", grants: [], instructions: defaultInstructions(kind, skill), grants_by: "suggested", skill_keys: [] };
}

/** seedMembers starts setup with a small team: a manager, a developer on its staff and an independent researcher. */
export function seedMembers(p: SetupPayload): SetupPayload {
  if (p.members.length) return p;
  const lead = member("manager", "Atlas");
  return { ...p, members: [lead, member("staff", "Pike", "developer", lead.key), member("staff", "Ivy", "researcher")], first_goal: { manager_key: lead.key, title: "", done_when: "" } };
}

/** tidyGrants keeps staff access within their manager's and refreshes suggestions. */
export function tidyGrants(p: SetupPayload, dirs: PickedDir[]): SetupPayload {
  const byKey = new Map(p.members.map((m) => [m.key, m]));
  const limitFor = (m: SetupMember) => {
    const boss = m.reports_to_key ? byKey.get(m.reports_to_key) : undefined;
    if (!boss) return null;
    return boss.all_directories ? dirs.map((d) => ({ directory_key: d.key, mode: "read_write" as const })) : boss.grants.map((g) => ({ directory_key: g.directory_key, mode: g.mode }));
  };
  const members = p.members.map((m) => {
    if (m.kind === "manager") return { ...m, grants: m.all_directories ? [] : m.grants.filter((g) => dirs.some((d) => d.key === g.directory_key)) };
    const limit = limitFor(m);
    if (m.grants_by === "suggested") {
      const s = suggest(m.skill, dirs, p.workspace.name, limit);
      return { ...m, grants: s.map((g) => ({ ...g, source: "suggested" as const })) };
    }
    return { ...m, grants: clamp(m.grants.map((g) => ({ ...g })), limit).map((g) => ({ ...g, source: "human" as const })) };
  });
  return { ...p, members };
}

export function Members({ p, set, dirs, errors }: StepProps) {
  const managers = p.members.filter((m) => m.kind === "manager");
  const independent = p.members.filter((m) => m.kind === "staff" && !m.reports_to_key);
  const update = (key: string, patch: Partial<SetupMember>) => set((x) => ({ ...x, members: x.members.map((m) => (m.key === key ? { ...m, ...patch } : m)) }));
  const remove = (key: string) => set((x) => ({
    ...x,
    members: x.members.filter((m) => m.key !== key).map((m) => (m.reports_to_key === key ? { ...m, reports_to_key: "" } : m)),
    people: x.people.map((pp) => ({ ...pp, manager_keys: pp.manager_keys.filter((k) => k !== key) })),
    first_goal: x.first_goal?.manager_key === key ? { ...x.first_goal, manager_key: x.members.find((m) => m.kind === "manager" && m.key !== key)?.key ?? "" } : x.first_goal,
  }));
  const add = (kind: "manager" | "staff", reports = "") => set((x) => ({ ...x, members: [...x.members, member(kind, "", kind === "staff" ? "developer" : "", reports)] }));

  return (
    <>
      <h1>Build your team</h1>
      <p className="lead">Your team is made of AI teammates. A <b>manager</b> plans the work and leads. <b>Staff</b> do the work. We’ve started you with a small team, so you can just press Continue, or change anyone below.</p>
      <div className="kindcards">
        <button type="button" className="kindcard" onClick={() => add("manager")}><b>+ Add a manager</b><span>Plans and leads. Takes goals from people and hands out tasks.</span></button>
        <button type="button" className="kindcard" onClick={() => add("staff", managers[0]?.key ?? "")}><b>+ Add staff</b><span>Does the work. Builds, reviews, researches or writes.</span></button>
      </div>
      {managers.map((mg) => (
        <div className="mgroup" key={mg.key}>
          <Row m={mg} p={p} update={update} remove={remove} canRemove={managers.length > 1} error={errors[`members.${p.members.indexOf(mg)}.name`]} />
          <Skills m={mg} p={p} update={update} />
          <Advanced m={mg}><Access m={mg} p={p} dirs={dirs} update={update} /><Instructions m={mg} update={update} /></Advanced>
          {p.members.filter((s) => s.kind === "staff" && s.reports_to_key === mg.key).map((s) => (
            <div className="mrow staff" style={{ border: 0, padding: "6px 0 0 22px" }} key={s.key}>
              <Row m={s} p={p} update={update} remove={remove} canRemove error={errors[`members.${p.members.indexOf(s)}.name`]} />
              <Skills m={s} p={p} update={update} />
              <Advanced m={s}><Access m={s} p={p} dirs={dirs} update={update} /><Instructions m={s} update={update} /></Advanced>
            </div>
          ))}
          <button type="button" className="btn btn-sm" style={{ borderStyle: "dashed", alignSelf: "flex-start" }} onClick={() => add("staff", mg.key)}>+ Add staff for {mg.name || "this manager"}</button>
        </div>
      ))}
      <div className="mgroup ind">
        <h3>Staff who help any manager</h3>
        <p className="hint" style={{ margin: 0 }}>Independent staff work for whichever manager needs them.</p>
        {independent.map((s) => (
          <div key={s.key} className="stack-sm">
            <Row m={s} p={p} update={update} remove={remove} canRemove error={errors[`members.${p.members.indexOf(s)}.name`]} />
            <Skills m={s} p={p} update={update} />
            <Advanced m={s}><Access m={s} p={p} dirs={dirs} update={update} /><Instructions m={s} update={update} /></Advanced>
          </div>
        ))}
        {!independent.length && <p className="hint" style={{ margin: 0 }}>None yet.</p>}
        <button type="button" className="btn btn-sm" style={{ borderStyle: "dashed", alignSelf: "flex-start" }} onClick={() => add("staff")}>+ Add staff for any manager</button>
      </div>
      {errors.members && <div className="err" style={{ fontSize: 13.5 }}>{errors.members}</div>}
      {Object.keys(errors).some((k) => k.startsWith("members.") && k.endsWith(".name")) && <div className="err" style={{ fontSize: 13.5 }}>Give every member a name.</div>}
    </>
  );
}

type RowProps = { m: SetupMember; p: SetupPayload; update: (k: string, patch: Partial<SetupMember>) => void; remove: (k: string) => void; canRemove: boolean; error?: string };

function Row({ m, p, update, remove, canRemove, error }: RowProps) {
  const managers = p.members.filter((x) => x.kind === "manager");
  return (
    <div className={"wrow" + (m.kind === "manager" ? " mgr" : "")}>
      <Avatar name={m.name || "?"} color={m.kind === "manager" ? "#14322A" : "#0E9AA7"} size={32} />
      <label className="field"><span className="lbl">{m.kind === "manager" ? "Manager name" : "Name"}</span>
        <input className="input" value={m.name} aria-invalid={!!error} onChange={(e) => update(m.key, { name: e.target.value })} placeholder={m.kind === "manager" ? "Atlas" : "Pike"} /></label>
      {m.kind === "staff" && (
        <>
          <label className="field"><span className="lbl">Role</span>
            <select className="input" value={m.skill} onChange={(e) => update(m.key, { skill: e.target.value, instructions: m.instructions === defaultInstructions("staff", m.skill) ? defaultInstructions("staff", e.target.value) : m.instructions })}>
              {Object.entries(SKILL_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select></label>
          <label className="field"><span className="lbl">Works for</span>
            <select className="input" value={m.reports_to_key} onChange={(e) => update(m.key, { reports_to_key: e.target.value })}>
              <option value="">Independent</option>
              {managers.map((x) => <option key={x.key} value={x.key}>{x.name || "Manager"}</option>)}
            </select></label>
        </>
      )}
      <label className="field"><span className="lbl">Model</span>
        <select className="input" value={m.model} onChange={(e) => update(m.key, { model: e.target.value })}>
          <option value="opus">Opus</option><option value="sonnet">Sonnet</option><option value="haiku">Haiku</option>
        </select></label>
      <button type="button" className="icon-btn" onClick={() => remove(m.key)} aria-label={`Remove ${m.name || "member"}`} disabled={!canRemove}>✕</button>
    </div>
  );
}

function Access({ m, p, dirs, update }: { m: SetupMember; p: SetupPayload; dirs: PickedDir[]; update: (k: string, patch: Partial<SetupMember>) => void }) {
  if (!dirs.length) return <div className="accrow"><span className="why">No directories yet. Add some on the Create a workspace step.</span></div>;
  if (m.kind === "manager") {
    return (
      <div className="accrow">
        <span className="k">Directories</span>
        <span className="seg" role="group" aria-label={`${m.name || "Manager"} directory access`}>
          <button type="button" aria-pressed={m.all_directories} onClick={() => update(m.key, { all_directories: true, grants: [] })}>All workspace directories</button>
          <button type="button" aria-pressed={!m.all_directories} onClick={() => update(m.key, { all_directories: false, grants: dirs.map((d) => ({ directory_key: d.key, mode: "read_write", source: "human", reason: "" })) })}>Only selected</button>
        </span>
        {!m.all_directories && dirs.map((d) => {
          const on = m.grants.some((g) => g.directory_key === d.key);
          return (
            <label className="dchip" key={d.key}><input type="checkbox" checked={on}
              onChange={(e) => update(m.key, { grants: e.target.checked ? [...m.grants, { directory_key: d.key, mode: "read_write", source: "human", reason: "" }] : m.grants.filter((g) => g.directory_key !== d.key) })} />{d.name}</label>
          );
        })}
      </div>
    );
  }
  const boss = m.reports_to_key ? p.members.find((x) => x.key === m.reports_to_key) : undefined;
  const pool = dirs.filter((d) => !boss || boss.all_directories || boss.grants.some((g) => g.directory_key === d.key));
  const by = boss ? boss.name || "Their manager" : "Canopy";
  const writes = ["developer", "designer", "writer"].includes(m.skill);
  return (
    <div className="accrow">
      <span className="k">Access</span>
      {pool.map((d) => {
        const g = m.grants.find((x) => x.directory_key === d.key);
        return (
          <label className="dchip" key={d.key}>
            <input type="checkbox" checked={!!g} onChange={(e) => update(m.key, {
              grants_by: "you",
              grants: e.target.checked ? [...m.grants, { directory_key: d.key, mode: writes ? "read_write" : "read", source: "human", reason: "" }] : m.grants.filter((x) => x.directory_key !== d.key),
            })} />{d.name}{g && <small>{g.mode === "read_write" ? "read & write" : "read"}</small>}
          </label>
        );
      })}
      {!pool.length && <span className="hint">Its manager has no directories to share</span>}
      {m.grants_by === "you" ? (
        <>
          <span className="pill">Changed by you</span>
          <button type="button" className="linkbtn" onClick={() => update(m.key, { grants_by: "suggested" })}>Ask {by} to suggest again</button>
        </>
      ) : <span className="pill info">Suggested by {by}</span>}
      <span className="why">{m.grants_by === "you" ? `Only the ticked directories are shared with ${m.name || "this member"}.` : `${m.name || "This member"} ${WHY[m.skill] ?? "gets only what it needs"}. Everything else stays hidden from it.`}</span>
    </div>
  );
}

function Instructions({ m, update }: { m: SetupMember; update: (k: string, patch: Partial<SetupMember>) => void }) {
  const [open, setOpen] = useState(false);
  const edited = m.instructions !== defaultInstructions(m.kind, m.skill);
  return (
    <details className="instr" open={open} onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}>
      <summary>Instructions for {m.name || "this member"} (Markdown){edited ? " · edited" : ""}</summary>
      <div className="pgrid" style={{ marginTop: 6 }}>
        <label className="field"><span className="lbl">Write</span><textarea value={m.instructions} onChange={(e) => update(m.key, { instructions: e.target.value })} aria-label={`Instructions for ${m.name || "member"}`} /></label>
        <div className="field"><span className="lbl">Preview</span><Markdown text={m.instructions} className="mdprev md" /></div>
      </div>
    </details>
  );
}

function Skills({ m, p, update }: { m: SetupMember; p: SetupPayload; update: (k: string, patch: Partial<SetupMember>) => void }) {
  if (!p.skills.length) return null;
  return (
    <div className="accrow">
      <span className="k">Skills</span>
      <SkillPicker skills={p.skills.map((s) => ({ id: s.key, name: s.name, source_kind: s.source_kind, source_url: s.source_url, content: s.content, updated_at: "" }))}
        selected={m.skill_keys} onToggle={(id, on) => update(m.key, { skill_keys: on ? [...m.skill_keys, id] : m.skill_keys.filter((k) => k !== id) })} />
    </div>
  );
}

/** Advanced tucks directory access and instructions away: the defaults are fine for most people. */
function Advanced({ m, children }: { m: SetupMember; children: React.ReactNode }) {
  return (
    <details className="instr">
      <summary>Directory access and instructions for {m.name || "this member"}</summary>
      <div className="stack-sm" style={{ marginTop: 6 }}>{children}</div>
    </details>
  );
}
