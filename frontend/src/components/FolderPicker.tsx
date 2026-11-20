"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";

type Entry = { name: string; path: string; is_repo?: boolean };
type Listing = { path: string; parent: string; home: string; entries: Entry[] };

/** FolderPicker browses folders on a person's connected computer and returns the ones they tick.
 * Git repositories are marked: they're shared through git, other folders sync to the hub. */
export function FolderPicker({ runnerId, computerName, label, have = [], onPick }: {
  runnerId: string; computerName: string; label: string; have?: string[]; onPick: (paths: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [list, setList] = useState<Listing | null>(null);
  const [picked, setPicked] = useState<string[]>([]);
  const [typed, setTyped] = useState("");
  const [error, setError] = useState("");

  async function load(path: string) {
    setError("");
    try {
      setList(await api<Listing>("GET", `/runners/${runnerId}/browse?path=${encodeURIComponent(path)}`));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Couldn’t list that folder.");
    }
  }
  const toggle = (p: string, on: boolean) => setPicked(on ? [...picked, p] : picked.filter((x) => x !== p));
  const done = () => {
    const all = [...picked, ...(typed.trim() ? [typed.trim()] : [])];
    if (all.length) onPick(all);
    setOpen(false);
    setPicked([]);
    setTyped("");
  };

  return (
    <>
      <button type="button" className="btn" onClick={() => { setOpen(true); void load(list?.path ?? ""); }}>{label}</button>
      {open && (
        <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && setOpen(false)} onKeyDown={(e) => e.key === "Escape" && setOpen(false)}>
          <div className="dialog" role="dialog" aria-label="Choose directories">
            <h2>Choose directories</h2>
            <p>From <b>{computerName}</b>. Git repositories are shared through git: agents work on their own branches and nothing uploads. Other folders sync to the hub, skipping what .gitignore, .dockerignore and .canopyignore exclude, build output and secret files such as .env and keys.</p>
            {list && (
              <div className="row">
                <code className="mono spacer" style={{ wordBreak: "break-all" }}>{list.path}</code>
                {list.parent !== list.path && <button className="btn btn-sm" type="button" onClick={() => void load(list.parent)}>↑ Up</button>}
                <button className="btn btn-sm" type="button" onClick={() => void load(list.home)}>Home</button>
              </div>
            )}
            {error && <div className="banner bad" role="alert">{error}</div>}
            <div className="pick-list">
              {list?.entries.map((e) => (
                <label key={e.path}>
                  <input type="checkbox" checked={have.includes(e.path) || picked.includes(e.path)} disabled={have.includes(e.path)} onChange={(ev) => toggle(e.path, ev.target.checked)} />
                  <span className="spacer">{e.name}</span>
                  {e.is_repo && <span className="pill">git</span>}
                  {!e.is_repo && <button type="button" className="linkbtn" onClick={(ev) => { ev.preventDefault(); void load(e.path); }}>Open</button>}
                </label>
              ))}
              {list && !list.entries.length && <p className="muted" style={{ padding: 8 }}>No folders here.</p>}
            </div>
            <label className="field"><span>Or type a path on {computerName}</span><input className="input mono" value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="~/code/another-project" /></label>
            <div className="row"><span className="spacer" />
              <button type="button" className="btn" onClick={() => setOpen(false)}>Cancel</button>
              <button type="button" className="btn btn-primary" disabled={!picked.length && !typed.trim()} onClick={done}>Add {picked.length + (typed.trim() ? 1 : 0) || ""}</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
