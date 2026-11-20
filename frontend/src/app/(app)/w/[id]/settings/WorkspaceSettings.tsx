"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { Avatar } from "@/components/board/icons";
import { Markdown } from "@/components/board/panels/bits";
import { FolderPicker } from "@/components/FolderPicker";
import { LogoUpload } from "@/components/LogoUpload";
import { Denied, PageShell, Section } from "@/components/PageShell";
import { api, ApiError, currentOrg, hardNavigate, isOrgAdmin } from "@/lib/api";
import { SkillAdd, SkillList, SkillPicker, SkillsIntro } from "@/components/skills/Skills";
import { useMe } from "@/lib/me";
import type { SkillView } from "@/lib/skills";
import { isHidden, SKILL_LABEL, type AnyMember, type CloneView, type Computer, type DirectoryDetail, type GrantView, type MemberView, type NoteView, type WorkspaceSummary, type WorkspaceView } from "@/lib/types";

type Detail = { workspace: WorkspaceView; viewer: { role: string }; members: AnyMember[]; directories: DirectoryDetail[]; grants: GrantView[]; clones: CloneView[] };
type Access = { user_id: string; name: string; email: string; avatar_color: string; role: string; manager_ids: string[]; independent_staff: boolean };
type Person = { id: string; name: string; email: string; avatar_color: string; role: string };

export function WorkspaceSettings() {
  return <Suspense><Inner /></Suspense>;
}

function Inner() {
  const { me } = useMe();
  const id = usePathname().split("/")[2] ?? "";
  const tab = useSearchParams().get("tab") ?? "general";
  const [d, setD] = useState<Detail | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    if (!id || id === "_") return;
    api<Detail>("GET", `/workspaces/${id}`).then(setD, (e) => setError(e instanceof ApiError ? e.message : "Couldn’t load the workspace."));
  }, [id]);
  useEffect(load, [load]);
  const summary: WorkspaceSummary | null = d ? { ...d.workspace, members: d.members.length, directories: d.directories.length, people: 0, role: d.viewer.role } : null;
  const title = d ? `${d.workspace.name} settings` : "Workspace settings";
  const admin = d?.viewer.role === "admin";
  const orgAdmin = isOrgAdmin(currentOrg(me)?.role);
  const run = async (fn: () => Promise<unknown>) => {
    setError("");
    try {
      await fn();
      load();
      return true;
    } catch (e) {
      setError(e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : "Something went wrong.");
      return false;
    }
  };

  return (
    <PageShell title={title} workspace={summary}>
      {!d ? (error ? <div className="note-box">{error}</div> : <p className="hint">Loading…</p>) : !admin ? <Denied title="Workspace settings" /> : (
        <>
          {error && <div className="banner bad">{error}</div>}
          {tab === "general" && <General d={d} run={run} />}
          {tab === "directories" && <Directories d={d} run={run} />}
          {tab === "members" && <Members d={d} run={run} />}
          {tab === "skills" && <Skills d={d} />}
          {tab === "access" && <AccessTab d={d} run={run} />}
          {tab === "danger" && (orgAdmin ? <Danger d={d} run={run} /> : <Denied title="Danger zone" />)}
        </>
      )}
    </PageShell>
  );
}

type Run = (fn: () => Promise<unknown>) => Promise<boolean>;

function General({ d, run }: { d: Detail; run: Run }) {
  const ws = d.workspace;
  const [name, setName] = useState(ws.name);
  const [about, setAbout] = useState(ws.about);
  const [notes, setNotes] = useState<NoteView[]>([]);
  const [open, setOpen] = useState<NoteView | "new" | null>(null);
  const loadNotes = useCallback(() => { api<{ notes: NoteView[] }>("GET", `/workspaces/${ws.id}/notes`).then((r) => setNotes(r.notes)); }, [ws.id]);
  useEffect(loadNotes, [loadNotes]);
  return (
    <>
      <h1>General</h1>
      <p className="lead">Settings for {ws.name}.</p>
      <Section title="Workspace">
        <form className="stack-sm" onSubmit={(e) => { e.preventDefault(); run(() => api("PATCH", `/workspaces/${ws.id}`, { name, about })); }}>
          <label className="field"><span>Name</span><input className="input" value={name} onChange={(e) => setName(e.target.value)} required /></label>
          <label className="field"><span>What it works on</span><textarea className="textarea" value={about} onChange={(e) => setAbout(e.target.value)} /></label>
          <div><button className="btn btn-primary">Save</button></div>
        </form>
        <LogoUpload base={`/workspaces/${ws.id}`} name={ws.name} url={ws.logo_url} onChange={() => run(async () => {})} />
        <div className="field">
          <span>When every task of a goal is done</span>
          <div className="seg" role="group" aria-label="Finishing goals">
            <button aria-pressed={ws.goal_completion === "auto"} onClick={() => run(() => api("PATCH", `/workspaces/${ws.id}`, { goal_completion: "auto" }))}>Mark done automatically</button>
            <button aria-pressed={ws.goal_completion !== "auto"} onClick={() => run(() => api("PATCH", `/workspaces/${ws.id}`, { goal_completion: "manual" }))}>Ask the goal’s person</button>
          </div>
          <span className="hint">Goals run on the computer of the person who gives them. Done goals’ tasks are hidden from the board.</span>
        </div>
      </Section>
      <Section title="Notes and decisions" sub="Kept in the hub, so every member can read them even when no computer is on. Managers write plans and summaries; people write decisions and notes.">
        {notes.map((n) => (
          <button className="trow" key={n.id} onClick={() => setOpen(n)}>
            <span className={`pill ${n.kind === "decision" ? "goal" : n.kind === "summary" ? "ok" : ""}`}>{n.kind[0].toUpperCase() + n.kind.slice(1)}</span>
            <span className="spacer">{n.title}</span>
          </button>
        ))}
        {!notes.length && <p className="hint">No notes yet.</p>}
        <div><button className="btn btn-sm" onClick={() => setOpen("new")}>+ Add a note</button></div>
      </Section>
      {open && <NoteDialog wsId={ws.id} note={open === "new" ? undefined : open} close={() => { setOpen(null); loadNotes(); }} />}
    </>
  );
}

function NoteDialog({ wsId, note, close }: { wsId: string; note?: NoteView; close: () => void }) {
  const [title, setTitle] = useState(note?.title ?? "");
  const [body, setBody] = useState(note?.body ?? "");
  const [kind, setKind] = useState(note?.kind === "decision" ? "decision" : "note");
  const [edit, setEdit] = useState(!note);
  const [error, setError] = useState("");
  const editable = !note || note.kind === "note" || note.kind === "decision";
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      if (note) await api("PATCH", `/notes/${note.id}`, { title, body, kind });
      else await api("POST", `/workspaces/${wsId}/notes`, { title, body, kind });
      close();
    } catch (err) {
      setError(err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : "Couldn’t save.");
    }
  };
  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="dialog" onSubmit={save} style={{ width: "min(640px,100%)" }}>
        {edit ? (
          <>
            <h2>{note ? "Edit note" : "Add a note"}</h2>
            <p>Notes are kept in the hub. Members read them when they plan and work.</p>
            <label className="field"><span>Kind</span><select className="input" value={kind} onChange={(e) => setKind(e.target.value)}><option value="note">Note</option><option value="decision">Decision</option></select></label>
            <label className="field"><span>Title</span><input className="input" value={title} onChange={(e) => setTitle(e.target.value)} required /></label>
            <label className="field"><span>Text (Markdown)</span><textarea className="textarea" rows={10} value={body} onChange={(e) => setBody(e.target.value)} /></label>
          </>
        ) : (
          <>
            <span className={`pill ${note!.kind === "decision" ? "goal" : note!.kind === "summary" ? "ok" : ""}`} style={{ alignSelf: "flex-start" }}>{note!.kind}</span>
            <h2>{note!.title}</h2>
            <Markdown text={note!.body} className="box md" />
          </>
        )}
        {error && <span className="err">{error}</span>}
        <div className="row">
          {note && <button type="button" className="btn btn-danger" onClick={async () => { await api("DELETE", `/notes/${note.id}`); close(); }}>Delete</button>}
          <span className="spacer" />
          <button type="button" className="btn" onClick={close}>Close</button>
          {edit ? <button className="btn btn-primary">Save note</button> : editable && <button type="button" className="btn btn-primary" onClick={() => setEdit(true)}>Edit</button>}
        </div>
      </form>
    </div>
  );
}

type Library = { skills: SkillView[]; member_skills: Record<string, string[]> };

/** useLibrary loads the workspace skill library and who has which skill. */
function useLibrary(wsId: string) {
  const [lib, setLib] = useState<Library>({ skills: [], member_skills: {} });
  const [ready, setReady] = useState(false);
  const reload = useCallback(() => api<Library>("GET", `/workspaces/${wsId}/skills`).then((r) => { setLib({ skills: r.skills, member_skills: r.member_skills ?? {} }); setReady(true); }, () => setReady(true)), [wsId]);
  useEffect(() => { reload(); }, [reload]);
  return { ...lib, ready, reload };
}

function Skills({ d }: { d: Detail }) {
  const { me } = useMe();
  const org = currentOrg(me);
  const ws = d.workspace.id;
  const lib = useLibrary(ws);
  const used = (s: SkillView) => Object.values(lib.member_skills).filter((ids) => ids.includes(s.id)).length;
  // Library calls throw so the form or row can show the hub's message beside what was typed.
  const call = async (fn: () => Promise<unknown>) => { await fn(); await lib.reload(); };
  return (
    <>
      <h1>Skills</h1>
      <SkillsIntro />
      <Section title="Skill library" sub={`Shared by everyone in ${d.workspace.name}. Give skills to members from the Members tab, from a member’s settings on the board, or let a manager give them to its own staff.`}>
        {lib.ready && <SkillList skills={lib.skills} usage={used}
          onRename={(s, name) => call(() => api("PATCH", `/skills/${s.id}`, { name }))}
          onDelete={(s) => call(() => api("DELETE", `/skills/${s.id}`))}
          onRefresh={(s) => call(() => api("PATCH", `/skills/${s.id}`, { refresh: true }))} />}
        {org && <SkillAdd orgId={org.id} taken={lib.skills.map((s) => s.name)}
          onAdd={(x) => call(() => api("POST", `/workspaces/${ws}/skills`, { name: x.name, source_kind: x.source_kind, ...(x.source_kind === "github" ? { source_url: x.source_url } : { content: x.content }) }))} />}
      </Section>
    </>
  );
}

function Directories({ d, run }: { d: Detail; run: Run }) {
  const { me } = useMe();
  const org = currentOrg(me);
  const [mine, setMine] = useState<Computer | null>(null);
  const [people, setPeople] = useState<Access[]>([]);
  useEffect(() => {
    if (!org) return;
    api<{ runners: Computer[]; my_computer: string | null }>("GET", `/orgs/${org.id}/runners`).then((r) => {
      setMine(r.runners.find((x) => x.id === r.my_computer) ?? r.runners.find((x) => x.owner_user_id === me.user.id && x.online) ?? null);
    });
    api<{ access: Access[] }>("GET", `/workspaces/${d.workspace.id}/access`).then((r) => setPeople(r.access), () => {});
  }, [org, me.user.id, d.workspace.id]);
  const add = (paths: string[]) => paths.reduce((p, path) => p.then(() => run(() => api("POST", `/workspaces/${d.workspace.id}/directories`, { path, runner_id: mine?.id }))), Promise.resolve(true));
  const members = d.members.filter((m) => !isHidden(m)) as MemberView[];

  return (
    <>
      <h1>Directories</h1>
      <p className="lead">Git repositories are shared through git: each person works in their own clone and agents work on their own branches. Other folders live on the computer of the person who added them; the hub keeps a synced copy so agents can read them while that computer is off.</p>
      <div className="row wrap">
        {mine ? <FolderPicker runnerId={mine.id} computerName={mine.name} label={`+ Add from ${mine.name}`} have={d.directories.map((x) => x.path)} onPick={add} />
          : <span className="note-box" style={{ background: "var(--amber-soft)" }}>Connect your computer in Computers and sign-ins to add directories from it.</span>}
      </div>
      {d.directories.map((dir) => {
        const clones = d.clones.filter((c) => c.directory_id === dir.id);
        const canEdit = dir.owner_user_id === me.user.id || isOrgAdmin(org?.role) || d.viewer.role === "admin";
        return (
          <section className="card panel" key={dir.id}>
            <div className="row"><h2 className="spacer mono" style={{ fontSize: 15 }}>{dir.name}</h2><span className="pill">{dir.is_repo ? "Git repository" : "Folder"}</span></div>
            <dl className="kv">
              <dt>Path</dt><dd className="mono">{dir.path}</dd>
              <dt>Added by</dt><dd>{dir.owner_name}</dd>
              {dir.is_repo ? (
                <>
                  <dt>Remote</dt><dd className="mono">{dir.remote}</dd>
                  <dt>Default branch</dt><dd className="mono">{dir.default_branch || "main"}</dd>
                  <dt>Clones</dt><dd>{clones.length ? clones.map((c) => <div key={c.runner_id}><span className="mono">{c.path}</span> <span className="hint">on {c.current_branch || "?"}{c.dirty ? `, ${c.dirty} uncommitted` : ""}</span></div>) : "None linked yet"}</dd>
                </>
              ) : (
                <><dt>Synced</dt><dd>Skips .gitignore, .dockerignore and .canopyignore entries, build output and secrets.</dd></>
              )}
            </dl>
            {dir.is_repo && canEdit && <RepoSetup dir={dir} run={run} wsId={d.workspace.id} />}
            <div className="stack-sm">
              <span className="lbl">People</span>
              {people.map((p) => {
                const mode = p.user_id === dir.owner_user_id ? "read_write" : dir.access?.[p.user_id] ?? "read";
                return (
                  <div className="row" key={p.user_id}>
                    <span className="spacer">{p.name}{p.user_id === dir.owner_user_id && <span className="pill" style={{ marginLeft: 6 }}>Added it</span>}</span>
                    {p.user_id === dir.owner_user_id ? <span className="hint">Read & write</span> : (
                      <select className="input" style={{ minHeight: 36, width: "auto" }} value={mode} disabled={!canEdit}
                        onChange={(e) => run(() => api("PUT", `/workspaces/${d.workspace.id}/directories/${dir.id}/access/${p.user_id}`, { mode: e.target.value }))}>
                        <option value="none">No access</option><option value="read">Read</option><option value="read_write">Read & write</option>
                      </select>
                    )}
                  </div>
                );
              })}
              <span className="hint">{dir.is_repo
                ? "Read & write lets agents working for that person commit to goal branches. Their own git sign-in decides what they can push."
                : `Someone without write access can still ask: the change waits for ${dir.owner_name.split(" ")[0]}’s approval and runs on their computer, counting toward their usage.`}</span>
            </div>
            <div className="stack-sm">
              <span className="lbl">Members</span>
              <div className="chips">
                {members.map((m) => {
                  const g = d.grants.find((x) => x.member_id === m.id && x.directory_id === dir.id);
                  return <span className="chip" key={m.id} data-m={g ? (g.mode === "read_write" ? "rw" : "read") : ""}>{m.name}{g && <small>{g.mode === "read_write" ? "read & write" : "read"}</small>}</span>;
                })}
              </div>
              <span className="hint">An agent’s access for a task is its own grant and the access of the person the task is for, whichever is narrower.</span>
            </div>
            {canEdit && <div><button className="btn btn-sm btn-danger" onClick={() => run(() => api("DELETE", `/workspaces/${d.workspace.id}/directories/${dir.id}`))}>Remove from workspace</button></div>}
          </section>
        );
      })}
      {!d.directories.length && <p className="hint">No directories yet. Members can only read the workspace notes.</p>}
    </>
  );
}

function RepoSetup({ dir, run, wsId }: { dir: DirectoryDetail; run: Run; wsId: string }) {
  const [setup, setSetup] = useState(dir.setup_command);
  const [copy, setCopy] = useState(dir.copy_files.join("\n"));
  return (
    <form className="stack-sm" onSubmit={(e) => { e.preventDefault(); run(() => api("PATCH", `/workspaces/${wsId}/directories/${dir.id}`, { setup_command: setup, copy_files: copy.split("\n").map((s) => s.trim()).filter(Boolean) })); }}>
      <div className="row wrap" style={{ alignItems: "flex-start" }}>
        <label className="field spacer"><span>Setup command <span style={{ fontWeight: 500 }}>(optional)</span></span><input className="input mono" value={setup} onChange={(e) => setSetup(e.target.value)} placeholder="pnpm install --offline" />
          <span className="hint">Runs once in each new worktree.</span></label>
        <label className="field spacer"><span>Files to copy into worktrees <span style={{ fontWeight: 500 }}>(optional)</span></span><textarea className="textarea mono" rows={2} value={copy} onChange={(e) => setCopy(e.target.value)} placeholder=".env.local" />
          <span className="hint">Copied from the person’s own clone on that computer only, never uploaded.</span></label>
      </div>
      <div><button className="btn btn-sm">Save</button></div>
    </form>
  );
}

function Members({ d, run }: { d: Detail; run: Run }) {
  const ws = d.workspace.id;
  const members = d.members.filter((m) => !isHidden(m)) as MemberView[];
  const managers = members.filter((m) => m.kind === "manager");
  const [form, setForm] = useState({ name: "", kind: "staff", skill: "developer", reports_to: "", model: "sonnet" });
  const [pick, setPick] = useState<string[]>([]);
  const [addError, setAddError] = useState("");
  const lib = useLibrary(ws);
  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    setAddError("");
    try {
      const r = await api<{ member: MemberView }>("POST", `/workspaces/${ws}/members`, {
        name: form.name, kind: form.kind, cli: "claude", model: form.model,
        ...(form.kind === "staff" ? { skill: form.skill, reports_to: form.reports_to || null, set_reports_to: true } : {}),
      });
      for (const id of pick) await api("PUT", `/workspaces/${ws}/members/${r.member.id}/skills/${id}`);
      setForm({ ...form, name: "" });
      setPick([]);
      await lib.reload();
      await run(async () => {});
    } catch (err) {
      setAddError(err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : "Couldn’t add the member.");
    }
  };
  const toggle = async (m: MemberView, id: string, on: boolean) => {
    await run(() => api(on ? "PUT" : "DELETE", `/workspaces/${ws}/members/${m.id}/skills/${id}`));
    lib.reload();
  };
  return (
    <>
      <h1>Members</h1>
      <p className="lead">The agents in {d.workspace.name}. Managers plan the work and lead staff; staff do the work. Each runs on the computer of whoever the work is for.</p>
      <section className="card panel">
        <h2>Add a teammate</h2>
        <form className="stack-sm" onSubmit={add}>
          <div className="kindcards" role="group" aria-label="Kind of teammate">
            {[["manager", "Manager", "Plans and leads. Takes goals from people and hands out tasks."], ["staff", "Staff", "Does the work. Builds, reviews, researches or writes."]].map(([k, t, line]) => (
              <button type="button" key={k} className="kindcard" aria-pressed={form.kind === k} onClick={() => setForm({ ...form, kind: k, model: k === "manager" ? "opus" : "sonnet" })}><b>{t}</b><span>{line}</span></button>
            ))}
          </div>
          <div className="row wrap" style={{ alignItems: "flex-end" }}>
            <label className="field spacer"><span>Name</span><input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required placeholder={form.kind === "manager" ? "Atlas" : "Morgan"} /></label>
            {form.kind === "staff" && (
              <>
                <label className="field"><span>Role</span><select className="input" value={form.skill} onChange={(e) => setForm({ ...form, skill: e.target.value })}>{Object.entries(SKILL_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></label>
                <label className="field"><span>Works for</span><select className="input" value={form.reports_to} onChange={(e) => setForm({ ...form, reports_to: e.target.value })}><option value="">Any manager</option>{managers.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}</select></label>
              </>
            )}
          </div>
          <div className="field"><span>Skills <span style={{ fontWeight: 500 }}>(optional)</span></span>
            <SkillPicker skills={lib.skills} selected={pick} onToggle={(id, on) => setPick((x) => on ? [...x, id] : x.filter((y) => y !== id))}
              emptyHint={<>No skills yet. <a href={`/w/${ws}/settings?tab=skills`}>Add some on the Skills tab.</a></>} /></div>
          {addError && <span className="err" role="alert">{addError}</span>}
          <div><button className="btn btn-primary">{form.kind === "manager" ? "Add manager" : "Add staff member"}</button></div>
        </form>
      </section>
      <section className="card panel">
        <div className="scroll-x">
          <table className="table">
            <thead><tr><th>Member</th><th>Role</th><th>Works for</th><th>Model</th><th>Skills</th><th>Directories</th><th /></tr></thead>
            <tbody>
              {members.map((m) => (
                <tr key={m.id}>
                  <td><div className="row"><Avatar name={m.name} color={m.avatar_color} size={28} /><b>{m.name}</b></div></td>
                  <td>{m.kind === "manager" ? "Manager" : SKILL_LABEL[m.skill ?? ""]}</td>
                  <td>{m.kind === "manager" ? "—" : m.reports_to ? members.find((x) => x.id === m.reports_to)?.name : "Independent"}</td>
                  <td><select className="input" style={{ minHeight: 34, width: "auto" }} value={m.model} onChange={(e) => run(() => api("PATCH", `/workspaces/${ws}/members/${m.id}`, { model: e.target.value }))}>
                    <option value="opus">Opus</option><option value="sonnet">Sonnet</option><option value="haiku">Haiku</option></select></td>
                  <td style={{ minWidth: 180 }}><SkillPicker skills={lib.skills} selected={lib.member_skills[m.id] ?? []} onToggle={(id, on) => toggle(m, id, on)}
                    emptyHint={<a href={`/w/${ws}/settings?tab=skills`}>Add skills</a>} /></td>
                  <td><div className="chips">{d.grants.filter((g) => g.member_id === m.id).map((g) => <span key={g.directory_id} className="chip" data-m={g.mode === "read_write" ? "rw" : "read"}>{d.directories.find((x) => x.id === g.directory_id)?.name}</span>)}
                    {m.all_directories && <span className="hint">All directories</span>}</div></td>
                  <td><div className="row" style={{ justifyContent: "flex-end" }}>
                    <a className="btn btn-sm" href={`/w/${ws}`}>Open chat on the board</a>
                    <button className="btn btn-sm btn-danger" disabled={m.kind === "manager" && managers.length <= 1} title={m.kind === "manager" && managers.length <= 1 ? "The last manager can’t be removed. Add another manager first." : undefined}
                      onClick={() => run(() => api("DELETE", `/workspaces/${ws}/members/${m.id}`, { open_tasks: "hand_to", hand_to: m.reports_to ?? managers.find((x) => x.id !== m.id)?.id ?? null }))}>Remove</button>
                  </div></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

function AccessTab({ d, run }: { d: Detail; run: Run }) {
  const { me } = useMe();
  const org = currentOrg(me);
  const ws = d.workspace.id;
  const [people, setPeople] = useState<Person[]>([]);
  const [access, setAccess] = useState<Access[]>([]);
  const load = useCallback(() => {
    if (!org) return;
    api<{ people: Person[] }>("GET", `/orgs/${org.id}/people`).then((r) => setPeople(r.people));
    api<{ access: Access[] }>("GET", `/workspaces/${ws}/access`).then((r) => setAccess(r.access));
  }, [org, ws]);
  useEffect(load, [load]);
  const managers = (d.members.filter((m) => !isHidden(m)) as MemberView[]).filter((m) => m.kind === "manager");
  const put = (p: Person, a: Partial<Access>) => {
    const cur = access.find((x) => x.user_id === p.id);
    const body = { role: a.role ?? cur?.role ?? "staff", manager_ids: a.manager_ids ?? cur?.manager_ids ?? [], independent_staff: a.independent_staff ?? cur?.independent_staff ?? false };
    run(() => api("PUT", `/workspaces/${ws}/access/${p.id}`, body)).then(load);
  };
  return (
    <>
      <h1>Access</h1>
      <p className="lead">Who can see {d.workspace.name}. Admins see everything; staff see only the managers’ teams you choose. Invite someone new from People.</p>
      <section className="card panel">
        <div className="scroll-x">
          <table className="table">
            <thead><tr><th>Person</th><th>Role here</th><th>Can see</th></tr></thead>
            <tbody>
              {people.map((p) => {
                const a = access.find((x) => x.user_id === p.id);
                return (
                  <tr key={p.id}>
                    <td><div className="row"><Avatar name={p.name} color={p.avatar_color} size={28} /><span>{p.name}<br /><span className="hint">{p.role === "owner" ? "Organization owner" : p.role === "admin" ? "Organization admin" : "Organization member"}</span></span></div></td>
                    <td>{p.role !== "member" ? <span className="pill ok">Admin</span> : (
                      <select className="input" style={{ minHeight: 36, width: "auto" }} value={a?.role ?? "none"}
                        onChange={(e) => e.target.value === "none" ? run(() => api("DELETE", `/workspaces/${ws}/access/${p.id}`)).then(load) : put(p, { role: e.target.value })}>
                        <option value="none">No access</option><option value="staff">Staff</option><option value="admin">Admin</option>
                      </select>
                    )}</td>
                    <td>{a?.role === "staff" && p.role === "member" ? (
                      <div className="row wrap" style={{ gap: 12 }}>
                        {managers.map((m) => (
                          <label className="toggle" style={{ fontSize: 13 }} key={m.id}><input type="checkbox" checked={a.manager_ids.includes(m.id)}
                            onChange={(e) => put(p, { manager_ids: e.target.checked ? [...a.manager_ids, m.id] : a.manager_ids.filter((x) => x !== m.id) })} /> {m.name}’s team</label>
                        ))}
                        <label className="toggle" style={{ fontSize: 13 }}><input type="checkbox" checked={a.independent_staff} onChange={(e) => put(p, { independent_staff: e.target.checked })} /> Independent staff</label>
                      </div>
                    ) : a || p.role !== "member" ? "Everything" : <span className="hint">Nothing</span>}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

function Danger({ d, run }: { d: Detail; run: Run }) {
  const [confirm, setConfirm] = useState("");
  return (
    <>
      <h1>Danger zone</h1>
      <p className="lead">Actions here affect everyone in {d.workspace.name}.</p>
      <Section title="Restart all sessions" sub="Stops every agent’s CLI and starts fresh conversations. Tasks in progress continue with a short handoff. Agents waiting on someone’s answer keep waiting.">
        <div><button className="btn" onClick={() => run(() => api("POST", `/workspaces/${d.workspace.id}/restart-sessions`))}>Restart all sessions</button></div>
      </Section>
      <Section title="Delete this workspace" sub="Deletes its members, goals, tasks, chats, notes and outputs. Repositories and folders stay on their owners’ computers; the hub copies are removed." danger>
        <form className="row wrap" style={{ alignItems: "flex-end" }} onSubmit={async (e) => { e.preventDefault(); if (await run(() => api("DELETE", `/workspaces/${d.workspace.id}`))) hardNavigate("/board"); }}>
          <label className="field spacer"><span>Type <b>{d.workspace.name}</b> to confirm</span><input className="input" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="off" /></label>
          <button className="btn btn-danger" disabled={confirm !== d.workspace.name}>Delete workspace</button>
        </form>
      </Section>
    </>
  );
}
