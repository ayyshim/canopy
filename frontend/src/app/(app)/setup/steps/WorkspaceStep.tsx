"use client";

import { RepoIcon } from "@/components/board/icons";
import { FolderPicker } from "@/components/FolderPicker";
import { api } from "@/lib/api";
import { newKey } from "@/lib/setup/state";
import { classify, dirName } from "@/lib/setup/suggest";
import type { StepProps } from "../Setup";

/** Create a workspace: its name and the repositories and folders picked on your computer. */
export function WorkspaceStep({ p, set, dirs, setDirs, computer, orgName, errors }: StepProps) {
  const add = async (paths: string[]) => {
    const fresh = paths.filter((x) => !dirs.some((d) => d.path === x));
    const picked = await Promise.all(fresh.map(async (path) => {
      const info = computer ? await api<{ is_repo: boolean; remote: string; current_branch: string }>("GET", `/runners/${computer.id}/repo?path=${encodeURIComponent(path)}`).catch(() => null) : null;
      return { key: newKey("d"), path, name: dirName(path), kind: classify(path), is_repo: !!info?.is_repo && !!info.remote, local_git: !!info?.is_repo && !info.remote, remote: info?.remote ?? "", branch: info?.current_branch ?? "" };
    }));
    setDirs([...dirs, ...picked]);
  };
  return (
    <>
      <h1>Create a workspace</h1>
      <p className="lead">A workspace in {orgName} holds manager and staff agents and the repositories and folders they can use.</p>
      <label className="field"><span>Workspace name</span>
        <input className="input" value={p.workspace.name} onChange={(e) => set((x) => ({ ...x, workspace: { ...x.workspace, name: e.target.value } }))} placeholder="Payments" aria-invalid={!!errors["workspace.name"]} autoFocus />
        {errors["workspace.name"] && <span className="err">{errors["workspace.name"]}</span>}
      </label>
      <label className="field"><span>What it works on <span style={{ fontWeight: 500 }}>(optional)</span></span>
        <input className="input" value={p.workspace.about} onChange={(e) => set((x) => ({ ...x, workspace: { ...x.workspace, about: e.target.value } }))} placeholder="The card payments API, its dashboard and docs" />
      </label>
      <h2>Repositories and folders</h2>
      <p className="hint" style={{ marginTop: -8 }}>Pick them on {computer?.name ?? "your computer"}. Git repositories are shared through git: agents work on their own branches and nothing uploads. Other folders sync to the hub so agents can read them while the computer is off.</p>
      {dirs.length ? (
        <ul className="dirs">
          {dirs.map((d) => (
            <li key={d.key}>
              {d.is_repo ? <RepoIcon /> : <span aria-hidden="true">📁</span>}
              <span className="path">{d.path}</span>
              {d.is_repo ? <span className="pill">Repository{d.branch ? ` · on ${d.branch}` : ""}</span> : <span className="pill ok" title={d.local_git ? "This git repository has no remote, so Canopy can’t share it through git. Add a remote to share it as a repository." : undefined}>{d.local_git ? "Git without a remote · syncs as a folder" : "Folder · syncs when created"}</span>}
              <select className="input" style={{ minHeight: 32, width: "auto" }} value={d.kind} aria-label={`Kind of ${d.name}`}
                onChange={(e) => setDirs(dirs.map((x) => (x.key === d.key ? { ...x, kind: e.target.value as typeof d.kind } : x)))}>
                <option value="code">Code</option><option value="docs">Docs</option><option value="design">Design</option><option value="other">Other</option>
              </select>
              <button className="icon-btn" onClick={() => setDirs(dirs.filter((x) => x.key !== d.key))} aria-label={`Remove ${d.path}`}>✕</button>
            </li>
          ))}
        </ul>
      ) : <p className="hint">None yet. Members can only read the workspace notes until you add some.</p>}
      {computer?.online ? (
        <div><FolderPicker runnerId={computer.id} computerName={computer.name} label={`+ Choose from ${computer.name}`} have={dirs.map((d) => d.path)} onPick={add} /></div>
      ) : <div className="note-box" style={{ background: "var(--amber-soft)" }}>Connect your computer to pick directories from it.</div>}
      <div className="note-box">Plans, notes, decisions and outputs are kept in the hub, so every member can read them even when this computer is off. Next you’ll decide which members can use each directory.</div>
    </>
  );
}
