"use client";

import { useRef, useState } from "react";
import { Markdown } from "@/components/board/panels/bits";
import { friendly, previewGithub, readMarkdownFile, type SkillDraft, type SkillView } from "@/lib/skills";

/** SkillsIntro says what a skill is, once, wherever skills are managed. */
export function SkillsIntro() {
  return (
    <p className="lead">
      A skill is a page of instructions that teaches an agent how you like things done, such as “our code review checklist” or “how we write release notes”.
      Add skills here once, then give them to any manager or staff member. Add a Markdown file, or paste a link to one on GitHub.
    </p>
  );
}

/** SkillAdd adds one skill: a Markdown file or pasted text, or a GitHub link that is fetched and previewed first. */
export function SkillAdd({ orgId, taken, onAdd }: { orgId: string; taken: string[]; onAdd: (d: SkillDraft) => Promise<void> }) {
  const [source, setSource] = useState<"md" | "github">("md");
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [content, setContent] = useState("");
  const [fetched, setFetched] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const file = useRef<HTMLInputElement>(null);

  const reset = () => { setName(""); setUrl(""); setContent(""); setFetched(false); setError(""); if (file.current) file.current.value = ""; };
  const dup = !!name.trim() && taken.some((t) => t.toLowerCase() === name.trim().toLowerCase());

  const pickFile = async (f?: File) => {
    if (!f) return;
    setError("");
    try {
      const r = await readMarkdownFile(f);
      setContent(r.content);
      setName((n) => n || r.name);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Couldn’t read that file.");
    }
  };
  const fetchLink = async () => {
    setError("");
    setBusy(true);
    try {
      const r = await previewGithub(orgId, url);
      setContent(r.content);
      setFetched(true);
      setName((n) => n || r.name);
    } catch (e) {
      setFetched(false);
      setContent("");
      setError(friendly(e, "Canopy couldn’t fetch that link. Check it and try again."));
    }
    setBusy(false);
  };
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await onAdd({ name: name.trim(), source_kind: source, source_url: source === "github" ? url.trim() : "", content });
      reset();
    } catch (err) {
      setError(friendly(err, "Couldn’t add the skill."));
    }
    setBusy(false);
  };
  const ready = !!name.trim() && !!content.trim() && !dup && (source === "md" || fetched);

  return (
    <form className="skill-add" onSubmit={submit}>
      <div className="row">
        <span className="lbl spacer">Add a skill</span>
        <div className="seg" role="group" aria-label="Where the skill comes from">
          <button type="button" aria-pressed={source === "md"} onClick={() => { setSource("md"); setError(""); }}>Markdown file or text</button>
          <button type="button" aria-pressed={source === "github"} onClick={() => { setSource("github"); setError(""); }}>GitHub link</button>
        </div>
      </div>
      {source === "md" ? (
        <>
          <div className="row wrap">
            <label className="btn btn-sm" style={{ cursor: "pointer" }}>Upload a .md file
              <input ref={file} type="file" accept=".md,.markdown,.txt,text/markdown,text/plain" hidden onChange={(e) => pickFile(e.target.files?.[0])} /></label>
            <span className="hint">or paste it below</span>
          </div>
          <textarea className="textarea mono" rows={5} style={{ fontSize: 12.5 }} value={content} onChange={(e) => { setContent(e.target.value); setError(""); }}
            placeholder={"# Code review checklist\n- Check error handling first\n- Ask for tests"} aria-label="Skill markdown" />
        </>
      ) : (
        <>
          <div className="row">
            <input className="input spacer" type="url" value={url} onChange={(e) => { setUrl(e.target.value); setFetched(false); setError(""); }}
              placeholder="https://github.com/your-team/skills/blob/main/review.md" aria-label="GitHub link" />
            <button type="button" className="btn" disabled={!url.trim() || busy} onClick={fetchLink}>{busy ? "Fetching…" : fetched ? "Fetch again" : "Fetch"}</button>
          </div>
          <span className="hint">Link to a single markdown file in a public repository. Canopy saves a copy; open the skill later to fetch it again.</span>
          {fetched && <div className="mdprev md skill-prev"><Markdown text={content} className="md" /></div>}
        </>
      )}
      <div className="row wrap" style={{ alignItems: "flex-end" }}>
        <label className="field spacer"><span>Name</span>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder="Code review checklist" aria-invalid={dup} /></label>
        <button className="btn btn-primary" disabled={!ready || busy}>Add skill</button>
      </div>
      {dup && <span className="err">You already have a skill with that name.</span>}
      {error && <span className="err" role="alert">{error}</span>}
    </form>
  );
}

/** SkillList shows the library: open one to read it, rename it, fetch it again or delete it. */
export function SkillList({ skills, onRename, onDelete, onRefresh, usage }: {
  skills: SkillView[];
  onRename: (s: SkillView, name: string) => Promise<void>;
  onDelete: (s: SkillView) => Promise<void>;
  onRefresh?: (s: SkillView) => Promise<void>;
  usage?: (s: SkillView) => number;
}) {
  const [open, setOpen] = useState("");
  const [names, setNames] = useState<Record<string, string>>({});
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<Record<string, string>>({});
  const guard = (id: string, fn: () => Promise<void>) => async () => {
    setError((e) => ({ ...e, [id]: "" }));
    try { await fn(); } catch (e) { setError((x) => ({ ...x, [id]: friendly(e) })); }
  };
  if (!skills.length) {
    return (
      <div className="empty-skills">
        <b>No skills yet</b>
        <span className="hint">Try a code review checklist, your style guide or release steps. Skills you add show up here and can be given to any member.</span>
      </div>
    );
  }
  return (
    <ul className="list skill-list">
      {skills.map((s) => {
        const isOpen = open === s.id;
        const n = usage?.(s) ?? 0;
        return (
          <li key={s.id} className="skill-item">
            <div className="row">
              <button type="button" className="linkbtn spacer" style={{ textAlign: "left", fontWeight: 600 }} aria-expanded={isOpen} onClick={() => setOpen(isOpen ? "" : s.id)}>
                {isOpen ? "▾" : "▸"} {s.name}</button>
              <span className="pill">{s.source_kind === "github" ? "GitHub" : "Markdown"}</span>
              {usage && <span className="hint">{n === 0 ? "Not given to anyone" : `${n} member${n === 1 ? "" : "s"}`}</span>}
            </div>
            {isOpen && (
              <div className="stack-sm" style={{ marginTop: 8 }}>
                <div className="row">
                  <input className="input spacer" aria-label="Skill name" value={names[s.id] ?? s.name} maxLength={80} onChange={(e) => setNames({ ...names, [s.id]: e.target.value })} />
                  <button type="button" className="btn btn-sm" disabled={!(names[s.id] ?? s.name).trim() || (names[s.id] ?? s.name) === s.name}
                    onClick={guard(s.id, () => onRename(s, (names[s.id] ?? s.name).trim()))}>Rename</button>
                </div>
                {s.source_kind === "github" && <span className="hint">From <a href={s.source_url} target="_blank" rel="noreferrer">{s.source_url}</a></span>}
                <div className="mdprev md skill-prev"><Markdown text={s.content} className="md" /></div>
                <div className="row">
                  {confirm === s.id
                    ? <><span className="hint spacer">Delete “{s.name}”? Members lose it.</span><button type="button" className="btn btn-sm" onClick={() => setConfirm("")}>Keep it</button>
                      <button type="button" className="btn btn-sm btn-danger" onClick={guard(s.id, () => onDelete(s))}>Delete</button></>
                    : <><span className="spacer" />
                      {s.source_kind === "github" && onRefresh && <button type="button" className="btn btn-sm" onClick={guard(s.id, () => onRefresh(s))}>Fetch again from GitHub</button>}
                      <button type="button" className="btn btn-sm btn-danger" onClick={() => setConfirm(s.id)}>Delete</button></>}
                </div>
                {error[s.id] && <span className="err" role="alert">{error[s.id]}</span>}
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

/** SkillPicker ticks library skills on and off for one member. */
export function SkillPicker({ skills, selected, onToggle, disabled, emptyHint }: {
  skills: SkillView[];
  selected: string[];
  onToggle: (id: string, on: boolean) => void;
  disabled?: boolean;
  emptyHint?: React.ReactNode;
}) {
  if (!skills.length) return <p className="hint" style={{ margin: 0 }}>{emptyHint ?? "No skills in the library yet."}</p>;
  return (
    <div className="chips" role="group" aria-label="Skills">
      {skills.map((s) => (
        <label className="dchip" key={s.id} title={s.content.slice(0, 200)}>
          <input type="checkbox" disabled={disabled} checked={selected.includes(s.id)} onChange={(e) => onToggle(s.id, e.target.checked)} />{s.name}
        </label>
      ))}
    </div>
  );
}
