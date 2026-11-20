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
      <h1>Name your workspace</h1>
      <p className="lead">A workspace is a space for one project or team in {orgName}. Give it a name and choose the files your team can use.</p>
      <label className="field"><span>Name</span>
        <input className="input" value={p.workspace.name} onChange={(e) => set((x) => ({ ...x, workspace: { ...x.workspace, name: e.target.value } }))} placeholder="Payments" aria-invalid={!!errors["workspace.name"]} autoFocus />
        {errors["workspace.name"] && <span className="err">{errors["workspace.name"]}</span>}
      </label>
      <label className="field"><span>What is it for? <span style={{ fontWeight: 500 }}>(optional)</span></span>
        <input className="input" value={p.workspace.about} onChange={(e) => set((x) => ({ ...x, workspace: { ...x.workspace, about: e.target.value } }))} placeholder="The card payments API, its dashboard and docs" />
      </label>
      <h2>Your files</h2>
      <p className="hint" style={{ marginTop: -8 }}>Choose the folders your team should work with. They stay on {computer?.name ?? "your computer"}.</p>
      {dirs.length ? (
        <ul className="dirs">
          {dirs.map((d) => (
            <li key={d.key}>
              {d.is_repo ? <RepoIcon /> : <span aria-hidden="true">📁</span>}
              <span className="path">{d.path}</span>
              {d.is_repo ? <span className="pill">Code project</span> : <span className="pill ok">Folder</span>}
              <button className="icon-btn" onClick={() => setDirs(dirs.filter((x) => x.key !== d.key))} aria-label={`Remove ${d.path}`}>✕</button>
            </li>
          ))}
        </ul>
      ) : <p className="hint">No folders yet. You can skip this and add some later.</p>}
      {dirs.length > 0 && (
        <details className="instr">
          <summary>Advanced: what is in each folder</summary>
          <div className="stack-sm" style={{ marginTop: 8 }}>
            <p className="hint">We guess from the folder name. Change it if we got it wrong.</p>
            {dirs.map((d) => (
              <label className="row" key={d.key}><span className="spacer">{d.name}</span>
                <select className="input" style={{ minHeight: 32, width: "auto" }} value={d.kind} aria-label={`What is in ${d.name}`}
                  onChange={(e) => setDirs(dirs.map((x) => (x.key === d.key ? { ...x, kind: e.target.value as typeof d.kind } : x)))}>
                  <option value="code">Code</option><option value="docs">Documents</option><option value="design">Design</option><option value="other">Other</option>
                </select></label>
            ))}
          </div>
        </details>
      )}
      {computer?.online ? (
        <div><FolderPicker runnerId={computer.id} computerName={computer.name} label="+ Choose folders" have={dirs.map((d) => d.path)} onPick={add} /></div>
      ) : <div className="note-box" style={{ background: "var(--amber-soft)" }}>Connect your computer first to choose folders.</div>}
      
    </>
  );
}
